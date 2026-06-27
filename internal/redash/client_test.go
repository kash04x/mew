package redash

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newClientForURL(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := NewClient(Options{BaseURL: baseURL, APIKey: "test-key", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return c
}

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return newClientForURL(t, ts.URL)
}

func TestRequestShape(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/session" {
			t.Errorf("got %s %s, want GET /api/session", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Key test-key" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		io.WriteString(w, `{"org_slug":"default","user":{"id":1,"name":"Akash","email":"akash@example.com"}}`)
	}))

	sess, err := c.Session(context.Background())
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if sess.User.Email != "akash@example.com" || sess.OrgSlug != "default" {
		t.Errorf("unexpected session: %+v", sess)
	}
}

func TestBaseURLWithPathPrefix(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/redash/api/data_sources" {
			t.Errorf("path = %q, want /redash/api/data_sources", r.URL.Path)
		}
		io.WriteString(w, `[]`)
	}))
	t.Cleanup(ts.Close)

	c := newClientForURL(t, ts.URL+"/redash/")
	if _, err := c.DataSources(context.Background()); err != nil {
		t.Fatalf("DataSources: %v", err)
	}
}

func TestAPIErrorParsingAndHint(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"message":"You don't have permission"}`)
	}))

	_, err := c.Session(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusForbidden || apiErr.Message != "You don't have permission" {
		t.Errorf("unexpected APIError: %+v", apiErr)
	}
	if apiErr.Hint() == "" {
		t.Error("403 should carry a hint")
	}
}

func TestRetriesIdempotentRequests(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"message":"temporarily down"}`)
			return
		}
		io.WriteString(w, `[]`)
	}))

	if _, err := c.DataSources(context.Background()); err != nil {
		t.Fatalf("DataSources after retries: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3", got)
	}
}

func TestDoesNotRetryPost(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"message":"boom"}`)
	}))

	_, _, err := c.ExecuteAdhoc(context.Background(), 1, `{"collection":"x"}`, 0)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 (no retry on POST)", got)
	}
}

func TestHonorsRetryAfter(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"message":"slow down"}`)
			return
		}
		io.WriteString(w, `[]`)
	}))

	var slept []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}

	if _, err := c.DataSources(context.Background()); err != nil {
		t.Fatalf("DataSources: %v", err)
	}
	if len(slept) != 1 || slept[0] != 3*time.Second {
		t.Fatalf("slept = %v, want exactly [3s]", slept)
	}
}

func TestStringEncodedResultData(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"query_result":{"id":5,"data":"{\"columns\":[{\"name\":\"n\"}],\"rows\":[{\"n\":1}]}"}}`)
	}))

	qr, err := c.QueryResultByID(context.Background(), 5)
	if err != nil {
		t.Fatalf("QueryResultByID: %v", err)
	}
	if len(qr.Data.Rows) != 1 || len(qr.Data.Columns) != 1 {
		t.Errorf("string-encoded data not decoded: %+v", qr.Data)
	}
}
