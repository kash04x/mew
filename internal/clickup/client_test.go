package clickup

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

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	c, err := NewClient(Options{APIToken: "pk_test", BaseURL: ts.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return c
}

func TestRequestShape(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/user" {
			t.Errorf("got %s %s, want GET /api/v2/user", r.Method, r.URL.Path)
		}
		// Personal tokens are sent bare, without a Bearer prefix.
		if got := r.Header.Get("Authorization"); got != "pk_test" {
			t.Errorf("Authorization = %q", got)
		}
		io.WriteString(w, `{"user":{"id":38927,"username":"Akash","email":"akash@example.com"}}`)
	}))

	u, err := c.User(context.Background())
	if err != nil {
		t.Fatalf("User: %v", err)
	}
	if u.ID != "38927" || u.Username != "Akash" {
		t.Errorf("unexpected user: %+v", u)
	}
}

func TestAPIErrorParsingAndHint(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"err":"Token invalid","ECODE":"OAUTH_025"}`)
	}))

	_, err := c.Teams(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Message != "Token invalid" || apiErr.ECode != "OAUTH_025" {
		t.Errorf("unexpected APIError: %+v", apiErr)
	}
	if apiErr.Hint() == "" {
		t.Error("401 should carry a hint")
	}
}

func TestRetriesOn429ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"err":"Rate limit reached"}`)
			return
		}
		io.WriteString(w, `{"teams":[{"id":"42","name":"smallcase"}]}`)
	}))

	teams, err := c.Teams(context.Background())
	if err != nil {
		t.Fatalf("Teams after retries: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3", got)
	}
	if len(teams) != 1 || teams[0].ID != "42" {
		t.Errorf("unexpected teams: %+v", teams)
	}
}

func TestDoesNotRetryPost(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"err":"boom"}`)
	}))

	_, err := c.CreateTask(context.Background(), "901", CreateTaskRequest{Name: "x"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 (no retry on POST)", got)
	}
}

func TestTaskFilterParams(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("page") != "2" || q.Get("include_closed") != "true" {
			t.Errorf("unexpected paging params: %v", q)
		}
		if got := q["statuses[]"]; len(got) != 2 || got[0] != "in progress" {
			t.Errorf("statuses[] = %v", got)
		}
		if q.Get("due_date_lt") != "1780000000000" {
			t.Errorf("due_date_lt = %q", q.Get("due_date_lt"))
		}
		if got := q["space_ids[]"]; len(got) != 1 || got[0] != "sp1" {
			t.Errorf("space_ids[] = %v", got)
		}
		io.WriteString(w, `{"tasks":[],"last_page":true}`)
	}))

	page, err := c.TeamTasks(context.Background(), "42", TaskFilters{
		Page:          2,
		Statuses:      []string{"in progress", "review"},
		IncludeClosed: true,
		DueBefore:     1780000000000,
		SpaceIDs:      []string{"sp1"},
	})
	if err != nil {
		t.Fatalf("TeamTasks: %v", err)
	}
	if !page.LastPage {
		t.Error("LastPage should be true")
	}
}

func TestTaskDecoding(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("include_subtasks") != "true" {
			t.Errorf("include_subtasks missing: %v", r.URL.Query())
		}
		io.WriteString(w, `{
			"id":"86abc","name":"Ship it","description":"do the thing",
			"status":{"status":"in progress","type":"custom"},
			"priority":{"priority":"high"},
			"due_date":"1780000000000","date_updated":1779000000000,
			"assignees":[{"id":1,"username":"Akash"}],
			"tags":[{"name":"backend"}],
			"custom_fields":[{"id":"cf1","name":"Sprint","type":"drop_down","value":3}],
			"list":{"id":"l1","name":"Backlog"},"url":"https://app.clickup.com/t/86abc"
		}`)
	}))

	task, err := c.Task(context.Background(), "86abc", true)
	if err != nil {
		t.Fatalf("Task: %v", err)
	}
	if task.Status.Status != "in progress" || task.Priority.Priority != "high" {
		t.Errorf("status/priority: %+v %+v", task.Status, task.Priority)
	}
	if task.DueDate != "1780000000000" || task.DateUpdated != "1779000000000" {
		t.Errorf("dates not normalized: %q %q", task.DueDate, task.DateUpdated)
	}
	if len(task.CustomFields) != 1 || task.CustomFields[0].Name != "Sprint" {
		t.Errorf("custom fields: %+v", task.CustomFields)
	}
}

func TestRateLimitWaitClamped(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("X-RateLimit-Reset", "1") // far in the past → clamp up
	if got := rateLimitWait(resp); got != time.Second {
		t.Errorf("past reset wait = %v, want 1s", got)
	}
	resp.Header.Del("X-RateLimit-Reset")
	resp.Header.Set("Retry-After", "600")
	if got := rateLimitWait(resp); got != time.Minute {
		t.Errorf("long wait = %v, want clamp to 1m", got)
	}
}
