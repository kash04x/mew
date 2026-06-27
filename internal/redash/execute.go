package redash

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// ErrQueryTimeout marks executions aborted because the configured query
// timeout elapsed before Redash finished the job.
var ErrQueryTimeout = errors.New("query execution timed out")

// QueryFailedError reports a job that Redash finished unsuccessfully.
type QueryFailedError struct {
	JobID   string
	Message string
}

func (e *QueryFailedError) Error() string {
	return "query execution failed: " + e.Message
}

// PollConfig tunes job polling.
type PollConfig struct {
	Initial    time.Duration
	Max        time.Duration
	Multiplier float64
}

// DefaultPollConfig polls eagerly at first and backs off for long jobs.
func DefaultPollConfig() PollConfig {
	return PollConfig{Initial: 500 * time.Millisecond, Max: 4 * time.Second, Multiplier: 1.6}
}

// ExecuteSavedQuery asks Redash to run a saved query. It returns either an
// immediately available (cached) result or a job to poll.
func (c *Client) ExecuteSavedQuery(ctx context.Context, queryID int, parameters map[string]any, maxAge int) (*QueryResult, *Job, error) {
	if parameters == nil {
		parameters = map[string]any{}
	}
	body := map[string]any{"parameters": parameters, "max_age": maxAge}
	var resp executeResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/queries/%d/results", queryID), nil, body, &resp); err != nil {
		return nil, nil, err
	}
	return splitExecuteResponse(resp)
}

// ExecuteAdhoc asks Redash to run raw query text against a data source.
func (c *Client) ExecuteAdhoc(ctx context.Context, dataSourceID int, queryText string, maxAge int) (*QueryResult, *Job, error) {
	body := map[string]any{
		"data_source_id": dataSourceID,
		"query":          queryText,
		"max_age":        maxAge,
		"parameters":     map[string]any{},
	}
	var resp executeResponse
	if err := c.do(ctx, http.MethodPost, "/query_results", nil, body, &resp); err != nil {
		return nil, nil, err
	}
	return splitExecuteResponse(resp)
}

func splitExecuteResponse(resp executeResponse) (*QueryResult, *Job, error) {
	switch {
	case resp.QueryResult != nil:
		return resp.QueryResult, nil, nil
	case resp.Job != nil:
		return nil, resp.Job, nil
	default:
		return nil, nil, errors.New("redash: execution response contained neither query_result nor job")
	}
}

// Job fetches the current state of an execution job.
func (c *Client) Job(ctx context.Context, jobID string) (*Job, error) {
	var resp jobResponse
	if err := c.do(ctx, http.MethodGet, "/jobs/"+url.PathEscape(jobID), nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Job, nil
}

// CancelJob asks Redash to cancel a running job.
func (c *Client) CancelJob(ctx context.Context, jobID string) error {
	return c.do(ctx, http.MethodDelete, "/jobs/"+url.PathEscape(jobID), nil, nil, nil)
}

// WaitForJob polls job until it finishes, then fetches the produced result.
// When ctx ends first, the job is cancelled in Redash on a best-effort basis
// so it does not keep loading the production database.
func (c *Client) WaitForJob(ctx context.Context, job *Job, poll PollConfig) (*QueryResult, error) {
	if poll.Initial <= 0 || poll.Max <= 0 || poll.Multiplier < 1 {
		poll = DefaultPollConfig()
	}
	delay := poll.Initial
	current := *job
	for {
		switch current.Status {
		case JobFinished:
			id, ok := current.resultID()
			if !ok {
				return nil, fmt.Errorf("redash: job %s finished without reporting a result id", current.ID)
			}
			return c.QueryResultByID(ctx, id)
		case JobFailed:
			msg := current.Error
			if msg == "" {
				msg = "Redash reported no error detail"
			}
			return nil, &QueryFailedError{JobID: current.ID, Message: msg}
		case JobCancelled:
			return nil, &QueryFailedError{JobID: current.ID, Message: "the job was cancelled in Redash"}
		}

		if err := c.sleep(ctx, delay); err != nil {
			return nil, c.abortJob(current.ID, err)
		}
		next, err := c.Job(ctx, current.ID)
		if err != nil {
			if ctx.Err() != nil {
				return nil, c.abortJob(current.ID, ctx.Err())
			}
			return nil, err
		}
		current = *next

		delay = min(time.Duration(float64(delay)*poll.Multiplier), poll.Max)
	}
}

// abortJob cancels jobID in Redash and translates cause for the caller.
func (c *Client) abortJob(jobID string, cause error) error {
	cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.CancelJob(cancelCtx, jobID); err != nil {
		c.logger.Warn("could not cancel redash job", "job_id", jobID, "error", err)
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return fmt.Errorf("%w: job %s was cancelled after the configured timeout; raise REDASH_QUERY_TIMEOUT_SECONDS for slow queries, or narrow the query", ErrQueryTimeout, jobID)
	}
	return cause
}

// RunSavedQuery executes a saved query end to end.
func (c *Client) RunSavedQuery(ctx context.Context, queryID int, parameters map[string]any, maxAge int, poll PollConfig) (*QueryResult, error) {
	result, job, err := c.ExecuteSavedQuery(ctx, queryID, parameters, maxAge)
	if err != nil {
		return nil, err
	}
	if result != nil {
		return result, nil
	}
	return c.WaitForJob(ctx, job, poll)
}

// RunAdhoc executes ad-hoc query text end to end.
func (c *Client) RunAdhoc(ctx context.Context, dataSourceID int, queryText string, maxAge int, poll PollConfig) (*QueryResult, error) {
	result, job, err := c.ExecuteAdhoc(ctx, dataSourceID, queryText, maxAge)
	if err != nil {
		return nil, err
	}
	if result != nil {
		return result, nil
	}
	return c.WaitForJob(ctx, job, poll)
}
