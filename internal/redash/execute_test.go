package redash

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunAdhocImmediateResult(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/query_results" {
			t.Errorf("got %s %s, want POST /api/query_results", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding body: %v", err)
		}
		if body["data_source_id"] != float64(7) {
			t.Errorf("data_source_id = %v, want 7", body["data_source_id"])
		}
		if body["max_age"] != float64(0) {
			t.Errorf("max_age = %v, want 0", body["max_age"])
		}
		io.WriteString(w, `{"query_result":{"id":1,"data":{"columns":[{"name":"n","type":"integer"}],"rows":[{"n":1},{"n":2}]},"runtime":0.05,"retrieved_at":"2026-06-12T00:00:00Z"}}`)
	}))

	qr, err := c.RunAdhoc(context.Background(), 7, `{"collection":"x"}`, 0, DefaultPollConfig())
	if err != nil {
		t.Fatalf("RunAdhoc: %v", err)
	}
	if len(qr.Data.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(qr.Data.Rows))
	}
}

func TestRunAdhocJobFlow(t *testing.T) {
	var jobPolls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/query_results", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"job":{"id":"j1","status":1}}`)
	})
	mux.HandleFunc("GET /api/jobs/j1", func(w http.ResponseWriter, r *http.Request) {
		if jobPolls.Add(1) == 1 {
			io.WriteString(w, `{"job":{"id":"j1","status":2}}`)
			return
		}
		io.WriteString(w, `{"job":{"id":"j1","status":3,"query_result_id":42}}`)
	})
	mux.HandleFunc("GET /api/query_results/42", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"query_result":{"id":42,"data":{"columns":[{"name":"n"}],"rows":[{"n":9}]}}}`)
	})

	c := newTestClient(t, mux)
	qr, err := c.RunAdhoc(context.Background(), 7, `{"collection":"x"}`, 0, DefaultPollConfig())
	if err != nil {
		t.Fatalf("RunAdhoc: %v", err)
	}
	if qr.ID != 42 {
		t.Fatalf("result id = %d, want 42", qr.ID)
	}
	if got := jobPolls.Load(); got != 2 {
		t.Fatalf("job polls = %d, want 2", got)
	}
}

func TestRunSavedQuerySendsParameters(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/queries/77/results" {
			t.Errorf("path = %q, want /api/queries/77/results", r.URL.Path)
		}
		var body struct {
			Parameters map[string]any `json:"parameters"`
			MaxAge     int            `json:"max_age"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding body: %v", err)
		}
		if body.Parameters["org"] != "acme" {
			t.Errorf("parameters = %v, want org=acme", body.Parameters)
		}
		io.WriteString(w, `{"query_result":{"id":5,"data":{"columns":[],"rows":[]}}}`)
	}))

	if _, err := c.RunSavedQuery(context.Background(), 77, map[string]any{"org": "acme"}, 0, DefaultPollConfig()); err != nil {
		t.Fatalf("RunSavedQuery: %v", err)
	}
}

func TestJobFailureSurfacesRedashError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/query_results", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"job":{"id":"j2","status":1}}`)
	})
	mux.HandleFunc("GET /api/jobs/j2", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"job":{"id":"j2","status":4,"error":"Error running query: collection not found"}}`)
	})

	c := newTestClient(t, mux)
	_, err := c.RunAdhoc(context.Background(), 7, `{"collection":"nope"}`, 0, DefaultPollConfig())
	var qfe *QueryFailedError
	if !errors.As(err, &qfe) {
		t.Fatalf("err = %v, want *QueryFailedError", err)
	}
	if !strings.Contains(qfe.Message, "collection not found") {
		t.Errorf("message = %q", qfe.Message)
	}
}

func TestJobResultIDFallbackField(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/query_results", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"job":{"id":"j3","status":3,"result_id":9}}`)
	})
	mux.HandleFunc("GET /api/query_results/9", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"query_result":{"id":9,"data":{"columns":[],"rows":[]}}}`)
	})

	c := newTestClient(t, mux)
	qr, err := c.RunAdhoc(context.Background(), 7, `{"collection":"x"}`, 0, DefaultPollConfig())
	if err != nil {
		t.Fatalf("RunAdhoc: %v", err)
	}
	if qr.ID != 9 {
		t.Fatalf("result id = %d, want 9 (via result_id field)", qr.ID)
	}
}

func TestTimeoutCancelsJobInRedash(t *testing.T) {
	var cancelled atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/query_results", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"job":{"id":"j9","status":1}}`)
	})
	mux.HandleFunc("GET /api/jobs/j9", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"job":{"id":"j9","status":2}}`)
	})
	mux.HandleFunc("DELETE /api/jobs/j9", func(w http.ResponseWriter, r *http.Request) {
		cancelled.Store(true)
		w.WriteHeader(http.StatusOK)
	})

	c := newTestClient(t, mux)
	var sleeps atomic.Int32
	c.sleep = func(ctx context.Context, _ time.Duration) error {
		if sleeps.Add(1) >= 2 {
			return context.DeadlineExceeded
		}
		return nil
	}

	_, err := c.RunAdhoc(context.Background(), 7, `{"collection":"x"}`, 0, DefaultPollConfig())
	if !errors.Is(err, ErrQueryTimeout) {
		t.Fatalf("err = %v, want ErrQueryTimeout", err)
	}
	if !cancelled.Load() {
		t.Fatal("the running job was not cancelled in Redash")
	}
}

func TestCancelledJobReported(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/query_results", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"job":{"id":"j4","status":5}}`)
	})

	c := newTestClient(t, mux)
	_, err := c.RunAdhoc(context.Background(), 7, `{"collection":"x"}`, 0, DefaultPollConfig())
	var qfe *QueryFailedError
	if !errors.As(err, &qfe) {
		t.Fatalf("err = %v, want *QueryFailedError", err)
	}
	if !strings.Contains(qfe.Message, "cancelled") {
		t.Errorf("message = %q, want mention of cancellation", qfe.Message)
	}
}
