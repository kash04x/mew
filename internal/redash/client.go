// Package redash is a typed client for the Redash REST API, written against
// Redash 10.1.0 and defensive about small cross-version differences.
package redash

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"mew/internal/version"
)

const (
	defaultTimeout    = 30 * time.Second
	defaultMaxRetries = 2
	// maxResponseBytes guards against unbounded result payloads.
	maxResponseBytes = 64 << 20
)

// Options configures a Client.
type Options struct {
	// BaseURL is the Redash root URL without the /api suffix.
	BaseURL string
	// APIKey is a Redash user API key, sent as "Authorization: Key ...".
	APIKey string
	// Timeout bounds each HTTP request. Defaults to 30s.
	Timeout time.Duration
	// MaxRetries is how often idempotent requests are retried on 429, 5xx
	// and transport errors. Zero means the default of 2; negative disables.
	MaxRetries int
	// Logger defaults to a no-op logger.
	Logger *slog.Logger
	// HTTPClient overrides the transport, mainly for tests. When set, its
	// own Timeout is respected as-is.
	HTTPClient *http.Client
}

// Client calls the Redash REST API.
type Client struct {
	baseURL    string
	apiKey     string
	httpc      *http.Client
	maxRetries int
	logger     *slog.Logger
	// sleep is replaceable in tests to avoid real delays.
	sleep func(context.Context, time.Duration) error
}

// NewClient validates opts and returns a Client.
func NewClient(opts Options) (*Client, error) {
	if strings.TrimSpace(opts.BaseURL) == "" {
		return nil, errors.New("redash: BaseURL is required")
	}
	if strings.TrimSpace(opts.APIKey) == "" {
		return nil, errors.New("redash: APIKey is required")
	}
	httpc := opts.HTTPClient
	if httpc == nil {
		timeout := opts.Timeout
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		httpc = &http.Client{Timeout: timeout}
	}
	retries := opts.MaxRetries
	if retries == 0 {
		retries = defaultMaxRetries
	}
	if retries < 0 {
		retries = 0
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Client{
		baseURL:    strings.TrimRight(opts.BaseURL, "/"),
		apiKey:     opts.APIKey,
		httpc:      httpc,
		maxRetries: retries,
		logger:     logger,
		sleep:      sleepCtx,
	}, nil
}

// APIError is a non-2xx response from Redash. The API key never appears in
// the error text.
type APIError struct {
	StatusCode int
	Message    string
	Method     string
	Path       string
	// RetryAfter is the delay requested by a 429 response, when present.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("redash: %s %s failed with status %d: %s", e.Method, e.Path, e.StatusCode, e.Message)
}

// Hint returns remediation advice for common statuses, or "".
func (e *APIError) Hint() string {
	switch e.StatusCode {
	case http.StatusUnauthorized:
		return "Redash rejected the API key; check REDASH_API_KEY (use the user API key from your Redash profile page)"
	case http.StatusForbidden:
		return "the API key's user lacks access to this object; check data source group permissions in Redash"
	case http.StatusNotFound:
		return "no such object; the id may be wrong, archived, or not visible to this user"
	case http.StatusTooManyRequests:
		return "Redash is rate limiting; retry later"
	}
	if e.StatusCode >= 500 {
		return "Redash itself failed; check the query syntax or the Redash instance health"
	}
	return ""
}

// do executes one API call, retrying idempotent methods on transient
// failures, and decodes the JSON response into out when out is non-nil.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("redash: encoding %s %s request: %w", method, path, err)
		}
	}
	fullURL := c.baseURL + "/api" + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	attempts := 1
	if method == http.MethodGet || method == http.MethodDelete {
		attempts += c.maxRetries
	}

	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			delay := retryDelay(attempt-1, err)
			c.logger.Debug("retrying redash request",
				"method", method, "path", path, "attempt", attempt, "delay", delay)
			if serr := c.sleep(ctx, delay); serr != nil {
				return serr
			}
		}
		if err = c.doOnce(ctx, method, fullURL, path, payload, out); err == nil {
			return nil
		}
		if ctx.Err() != nil || !shouldRetry(err) {
			return err
		}
	}
	return err
}

func (c *Client) doOnce(ctx context.Context, method, fullURL, path string, payload []byte, out any) error {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return fmt.Errorf("redash: building %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Key "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mew-redash/"+version.Version)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("redash: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("redash: reading %s %s response: %w", method, path, err)
	}
	if len(raw) > maxResponseBytes {
		return fmt.Errorf("redash: %s %s response exceeded %d MiB; narrow the query before retrying", method, path, maxResponseBytes>>20)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newAPIError(method, path, resp, raw)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("redash: decoding %s %s response: %w", method, path, err)
	}
	return nil
}

func newAPIError(method, path string, resp *http.Response, raw []byte) *APIError {
	apiErr := &APIError{StatusCode: resp.StatusCode, Method: method, Path: path}
	var parsed struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(raw, &parsed) == nil {
		if parsed.Message != "" {
			apiErr.Message = parsed.Message
		} else {
			apiErr.Message = parsed.Error
		}
	}
	if apiErr.Message == "" {
		apiErr.Message = clip(strings.TrimSpace(string(raw)), 300)
	}
	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(resp.StatusCode)
	}
	if s := resp.Header.Get("Retry-After"); s != "" {
		if secs, err := strconv.Atoi(s); err == nil && secs > 0 {
			apiErr.RetryAfter = time.Duration(secs) * time.Second
		}
	}
	return apiErr
}

// shouldRetry reports whether err is worth retrying for an idempotent call.
// Context errors are filtered by the caller.
func shouldRetry(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500
	}
	// A 200 with a malformed body will not improve on retry.
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) {
		return false
	}
	return true
}

// retryDelay backs off exponentially with jitter, honoring Retry-After.
func retryDelay(retry int, lastErr error) time.Duration {
	var apiErr *APIError
	if errors.As(lastErr, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}
	base := min(250*time.Millisecond*time.Duration(1<<(retry-1)), 2*time.Second)
	return base + time.Duration(rand.Int64N(int64(base/2)))
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// clip shortens s to at most max bytes without splitting a UTF-8 rune,
// appending an ellipsis when cut.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// Session returns the authenticated user, proving the base URL and key work.
func (c *Client) Session(ctx context.Context) (*Session, error) {
	var s Session
	if err := c.do(ctx, http.MethodGet, "/session", nil, nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// DataSources lists the data sources visible to the user.
func (c *Client) DataSources(ctx context.Context) ([]DataSource, error) {
	var out []DataSource
	if err := c.do(ctx, http.MethodGet, "/data_sources", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Schema returns the cached schema of a data source: for MongoDB, its
// collections and the fields Redash has observed in them.
func (c *Client) Schema(ctx context.Context, dataSourceID int) ([]SchemaTable, error) {
	var resp schemaResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/data_sources/%d/schema", dataSourceID), nil, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Schema, nil
}

// QueriesParams filters saved-query listings.
type QueriesParams struct {
	Search   string
	Tags     []string
	Page     int
	PageSize int
}

// Queries lists or searches saved queries.
func (c *Client) Queries(ctx context.Context, p QueriesParams) (*Page[Query], error) {
	q := url.Values{}
	q.Set("page", strconv.Itoa(p.Page))
	q.Set("page_size", strconv.Itoa(p.PageSize))
	if p.Search != "" {
		q.Set("q", p.Search)
	} else {
		// Redash orders by search rank when q is present; otherwise ask for
		// the most recently updated queries first.
		q.Set("order", "-updated_at")
	}
	for _, t := range p.Tags {
		q.Add("tags", t)
	}
	var page Page[Query]
	if err := c.do(ctx, http.MethodGet, "/queries", q, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// QueryByID fetches a saved query in full detail.
func (c *Client) QueryByID(ctx context.Context, id int) (*Query, error) {
	var out Query
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/queries/%d", id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DashboardsParams filters dashboard listings.
type DashboardsParams struct {
	Search   string
	Page     int
	PageSize int
}

// Dashboards lists or searches dashboards.
func (c *Client) Dashboards(ctx context.Context, p DashboardsParams) (*Page[Dashboard], error) {
	q := url.Values{}
	q.Set("page", strconv.Itoa(p.Page))
	q.Set("page_size", strconv.Itoa(p.PageSize))
	if p.Search != "" {
		q.Set("q", p.Search)
	} else {
		q.Set("order", "-updated_at")
	}
	var page Page[Dashboard]
	if err := c.do(ctx, http.MethodGet, "/dashboards", q, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// Dashboard fetches one dashboard with its widgets. Redash 10 addresses
// dashboards by numeric id; slugs go through the legacy flag.
func (c *Client) Dashboard(ctx context.Context, idOrSlug string) (*Dashboard, error) {
	q := url.Values{}
	if !allDigits(idOrSlug) {
		q.Set("legacy", "true")
	}
	var out Dashboard
	if err := c.do(ctx, http.MethodGet, "/dashboards/"+url.PathEscape(idOrSlug), q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// QueryResultByID fetches a stored query result.
func (c *Client) QueryResultByID(ctx context.Context, id int) (*QueryResult, error) {
	var resp queryResultResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/query_results/%d", id), nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.QueryResult, nil
}

// LatestQueryResult fetches the most recent cached result of a saved query
// without triggering execution.
func (c *Client) LatestQueryResult(ctx context.Context, queryID int) (*QueryResult, error) {
	var resp queryResultResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/queries/%d/results", queryID), nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.QueryResult, nil
}
