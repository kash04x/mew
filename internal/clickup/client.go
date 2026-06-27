// Package clickup is a typed client for the ClickUp REST API v2,
// authenticated with a personal API token.
package clickup

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

	"mew/internal/version"
)

const (
	defaultBaseURL    = "https://api.clickup.com"
	defaultTimeout    = 30 * time.Second
	defaultMaxRetries = 2
	// maxResponseBytes guards against unbounded payloads.
	maxResponseBytes = 16 << 20
)

// Options configures a Client.
type Options struct {
	// APIToken is a ClickUp personal API token (starts with pk_).
	APIToken string
	// BaseURL overrides the API host, mainly for tests. Defaults to the
	// public ClickUp API.
	BaseURL string
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

// Client calls the ClickUp REST API.
type Client struct {
	baseURL    string
	apiToken   string
	httpc      *http.Client
	maxRetries int
	logger     *slog.Logger
	// sleep is replaceable in tests to avoid real delays.
	sleep func(context.Context, time.Duration) error
}

// NewClient validates opts and returns a Client.
func NewClient(opts Options) (*Client, error) {
	if strings.TrimSpace(opts.APIToken) == "" {
		return nil, errors.New("clickup: APIToken is required")
	}
	baseURL := strings.TrimRight(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
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
		baseURL:    baseURL,
		apiToken:   opts.APIToken,
		httpc:      httpc,
		maxRetries: retries,
		logger:     logger,
		sleep:      sleepCtx,
	}, nil
}

// APIError is a non-2xx response from ClickUp. The API token never appears
// in the error text.
type APIError struct {
	StatusCode int
	Message    string
	// ECode is ClickUp's machine-readable error code (e.g. OAUTH_023).
	ECode  string
	Method string
	Path   string
	// RetryAfter is the wait suggested by a 429 response, when derivable.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	msg := e.Message
	if e.ECode != "" {
		msg = fmt.Sprintf("%s [%s]", msg, e.ECode)
	}
	return fmt.Sprintf("clickup: %s %s failed with status %d: %s", e.Method, e.Path, e.StatusCode, msg)
}

// Hint returns remediation advice for common statuses, or "".
func (e *APIError) Hint() string {
	switch e.StatusCode {
	case http.StatusUnauthorized:
		return "ClickUp rejected the token; check CLICKUP_API_TOKEN (ClickUp → Settings → Apps → API Token, starts with pk_)"
	case http.StatusForbidden:
		return "the token's user lacks access to this object; check your ClickUp workspace membership and permissions"
	case http.StatusNotFound:
		return "no such object; the id may be wrong, or it lives in a Workspace this token cannot see"
	case http.StatusTooManyRequests:
		return "ClickUp is rate limiting (100 requests/minute on most plans); retry later"
	}
	if e.StatusCode >= 500 {
		return "ClickUp itself failed; retry later or check status.clickup.com"
	}
	return ""
}

// do executes one API call against /api/v2, retrying idempotent methods on
// transient failures, and decodes the JSON response into out when non-nil.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("clickup: encoding %s %s request: %w", method, path, err)
		}
	}
	fullURL := c.baseURL + "/api/v2" + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	attempts := 1
	if method == http.MethodGet {
		attempts += c.maxRetries
	}

	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			delay := retryDelay(attempt-1, err)
			c.logger.Debug("retrying clickup request",
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
		return fmt.Errorf("clickup: building %s %s: %w", method, path, err)
	}
	// Personal tokens are sent bare, without a Bearer prefix.
	req.Header.Set("Authorization", c.apiToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mew-clickup/"+version.Version)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("clickup: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("clickup: reading %s %s response: %w", method, path, err)
	}
	if len(raw) > maxResponseBytes {
		return fmt.Errorf("clickup: %s %s response exceeded %d MiB; narrow the request before retrying", method, path, maxResponseBytes>>20)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newAPIError(method, path, resp, raw)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("clickup: decoding %s %s response: %w", method, path, err)
	}
	return nil
}

func newAPIError(method, path string, resp *http.Response, raw []byte) *APIError {
	apiErr := &APIError{StatusCode: resp.StatusCode, Method: method, Path: path}
	var parsed struct {
		Err   string `json:"err"`
		ECode string `json:"ECODE"`
	}
	if json.Unmarshal(raw, &parsed) == nil {
		apiErr.Message = parsed.Err
		apiErr.ECode = parsed.ECode
	}
	if apiErr.Message == "" {
		apiErr.Message = clip(strings.TrimSpace(string(raw)), 300)
	}
	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(resp.StatusCode)
	}
	apiErr.RetryAfter = rateLimitWait(resp)
	return apiErr
}

// rateLimitWait derives a wait from Retry-After or ClickUp's
// X-RateLimit-Reset (unix seconds) header, clamped to [1s, 60s].
func rateLimitWait(resp *http.Response) time.Duration {
	if s := resp.Header.Get("Retry-After"); s != "" {
		if secs, err := strconv.Atoi(s); err == nil && secs > 0 {
			return clampWait(time.Duration(secs) * time.Second)
		}
	}
	if s := resp.Header.Get("X-RateLimit-Reset"); s != "" {
		if reset, err := strconv.ParseInt(s, 10, 64); err == nil && reset > 0 {
			return clampWait(time.Until(time.Unix(reset, 0)))
		}
	}
	return 0
}

func clampWait(d time.Duration) time.Duration {
	if d < time.Second {
		return time.Second
	}
	if d > time.Minute {
		return time.Minute
	}
	return d
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

// retryDelay backs off exponentially with jitter, honoring rate-limit waits.
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

func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// User returns the token's authenticated user.
func (c *Client) User(ctx context.Context) (*User, error) {
	var resp userResponse
	if err := c.do(ctx, http.MethodGet, "/user", nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.User, nil
}

// Teams lists the workspaces the token can access.
func (c *Client) Teams(ctx context.Context) ([]Team, error) {
	var resp teamsResponse
	if err := c.do(ctx, http.MethodGet, "/team", nil, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Teams, nil
}

// Spaces lists the non-archived spaces of a workspace.
func (c *Client) Spaces(ctx context.Context, teamID string) ([]Space, error) {
	q := url.Values{"archived": {"false"}}
	var resp spacesResponse
	if err := c.do(ctx, http.MethodGet, "/team/"+url.PathEscape(teamID)+"/space", q, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Spaces, nil
}

// Folders lists a space's folders with their lists embedded.
func (c *Client) Folders(ctx context.Context, spaceID string) ([]Folder, error) {
	q := url.Values{"archived": {"false"}}
	var resp foldersResponse
	if err := c.do(ctx, http.MethodGet, "/space/"+url.PathEscape(spaceID)+"/folder", q, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Folders, nil
}

// FolderlessLists lists the lists sitting directly under a space.
func (c *Client) FolderlessLists(ctx context.Context, spaceID string) ([]List, error) {
	q := url.Values{"archived": {"false"}}
	var resp listsResponse
	if err := c.do(ctx, http.MethodGet, "/space/"+url.PathEscape(spaceID)+"/list", q, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Lists, nil
}

// TaskFilters narrows task listings. Zero values mean "no filter".
type TaskFilters struct {
	Page          int
	Statuses      []string
	AssigneeIDs   []string
	Tags          []string
	IncludeClosed bool
	Subtasks      bool
	// DueBefore/DueAfter are epoch milliseconds.
	DueBefore int64
	DueAfter  int64
	OrderBy   string
	// SpaceIDs/ListIDs apply only to workspace-wide search.
	SpaceIDs []string
	ListIDs  []string
}

func (f TaskFilters) values(teamWide bool) url.Values {
	q := url.Values{}
	q.Set("page", strconv.Itoa(max(f.Page, 0)))
	for _, s := range f.Statuses {
		q.Add("statuses[]", s)
	}
	for _, a := range f.AssigneeIDs {
		q.Add("assignees[]", a)
	}
	for _, t := range f.Tags {
		q.Add("tags[]", t)
	}
	if f.IncludeClosed {
		q.Set("include_closed", "true")
	}
	if f.Subtasks {
		q.Set("subtasks", "true")
	}
	if f.DueBefore > 0 {
		q.Set("due_date_lt", strconv.FormatInt(f.DueBefore, 10))
	}
	if f.DueAfter > 0 {
		q.Set("due_date_gt", strconv.FormatInt(f.DueAfter, 10))
	}
	if f.OrderBy != "" {
		q.Set("order_by", f.OrderBy)
	}
	if teamWide {
		for _, s := range f.SpaceIDs {
			q.Add("space_ids[]", s)
		}
		for _, l := range f.ListIDs {
			q.Add("list_ids[]", l)
		}
	}
	return q
}

// TasksPage is one page of tasks (ClickUp pages hold up to 100).
type TasksPage struct {
	Tasks    []Task
	LastPage bool
}

func tasksPage(resp tasksResponse) *TasksPage {
	p := &TasksPage{Tasks: resp.Tasks, LastPage: true}
	if resp.LastPage != nil {
		p.LastPage = *resp.LastPage
	} else if len(resp.Tasks) == 100 {
		p.LastPage = false
	}
	return p
}

// ListTasks lists tasks in one list.
func (c *Client) ListTasks(ctx context.Context, listID string, f TaskFilters) (*TasksPage, error) {
	var resp tasksResponse
	if err := c.do(ctx, http.MethodGet, "/list/"+url.PathEscape(listID)+"/task", f.values(false), nil, &resp); err != nil {
		return nil, err
	}
	return tasksPage(resp), nil
}

// TeamTasks searches tasks across a whole workspace.
func (c *Client) TeamTasks(ctx context.Context, teamID string, f TaskFilters) (*TasksPage, error) {
	var resp tasksResponse
	if err := c.do(ctx, http.MethodGet, "/team/"+url.PathEscape(teamID)+"/task", f.values(true), nil, &resp); err != nil {
		return nil, err
	}
	return tasksPage(resp), nil
}

// Task fetches one task in full detail.
func (c *Client) Task(ctx context.Context, taskID string, includeSubtasks bool) (*Task, error) {
	q := url.Values{}
	if includeSubtasks {
		q.Set("include_subtasks", "true")
	}
	var t Task
	if err := c.do(ctx, http.MethodGet, "/task/"+url.PathEscape(taskID), q, nil, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Comments returns a task's newest comments (ClickUp returns up to 25).
func (c *Client) Comments(ctx context.Context, taskID string) ([]Comment, error) {
	var resp commentsResponse
	if err := c.do(ctx, http.MethodGet, "/task/"+url.PathEscape(taskID)+"/comment", nil, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Comments, nil
}

// CreateTaskRequest is the body of POST /list/{id}/task. Dates are epoch
// milliseconds; Priority is 1=urgent 2=high 3=normal 4=low.
type CreateTaskRequest struct {
	Name                string   `json:"name"`
	MarkdownDescription string   `json:"markdown_description,omitempty"`
	Assignees           []int64  `json:"assignees,omitempty"`
	Tags                []string `json:"tags,omitempty"`
	Status              string   `json:"status,omitempty"`
	Priority            int      `json:"priority,omitempty"`
	DueDate             int64    `json:"due_date,omitempty"`
	DueDateTime         bool     `json:"due_date_time,omitempty"`
	Parent              string   `json:"parent,omitempty"`
	NotifyAll           bool     `json:"notify_all,omitempty"`
}

// CreateTask creates a task in a list.
func (c *Client) CreateTask(ctx context.Context, listID string, req CreateTaskRequest) (*Task, error) {
	var t Task
	if err := c.do(ctx, http.MethodPost, "/list/"+url.PathEscape(listID)+"/task", nil, req, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// AssigneeChanges adds and removes assignees by user id.
type AssigneeChanges struct {
	Add []int64 `json:"add,omitempty"`
	Rem []int64 `json:"rem,omitempty"`
}

// UpdateTaskRequest is the body of PUT /task/{id}; nil fields are untouched.
type UpdateTaskRequest struct {
	Name        *string          `json:"name,omitempty"`
	Description *string          `json:"description,omitempty"`
	Status      *string          `json:"status,omitempty"`
	Priority    *int             `json:"priority,omitempty"`
	DueDate     *int64           `json:"due_date,omitempty"`
	Assignees   *AssigneeChanges `json:"assignees,omitempty"`
}

// UpdateTask updates fields of a task.
func (c *Client) UpdateTask(ctx context.Context, taskID string, req UpdateTaskRequest) (*Task, error) {
	var t Task
	if err := c.do(ctx, http.MethodPut, "/task/"+url.PathEscape(taskID), nil, req, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// AddComment posts a comment on a task and returns the new comment id.
func (c *Client) AddComment(ctx context.Context, taskID, text string, notifyAll bool) (string, error) {
	body := map[string]any{"comment_text": text, "notify_all": notifyAll}
	var resp struct {
		ID FlexString `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/task/"+url.PathEscape(taskID)+"/comment", nil, body, &resp); err != nil {
		return "", err
	}
	return string(resp.ID), nil
}
