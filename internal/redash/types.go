package redash

import (
	"encoding/json"
	"fmt"
)

// BoolLike unmarshals JSON booleans as well as the 0/1 integers some Redash
// versions emit for boolean-ish fields (e.g. data source "paused").
type BoolLike bool

func (b *BoolLike) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case "true":
		*b = true
		return nil
	case "false", "null":
		*b = false
		return nil
	}
	var n float64
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("expected boolean-like value, got %s", data)
	}
	*b = n != 0
	return nil
}

// DataSource is an entry of GET /api/data_sources.
type DataSource struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Syntax      string   `json:"syntax"`
	Paused      BoolLike `json:"paused"`
	PauseReason string   `json:"pause_reason"`
	ViewOnly    bool     `json:"view_only"`
}

// SchemaColumn tolerates both plain string columns (Redash 10) and
// {name, type} objects (newer Redash versions).
type SchemaColumn struct {
	Name string
	Type string
}

func (c *SchemaColumn) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		c.Name = s
		return nil
	}
	var obj struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("unsupported schema column encoding %s", data)
	}
	c.Name, c.Type = obj.Name, obj.Type
	return nil
}

// SchemaTable is one table (for MongoDB: one collection) of a data source
// schema.
type SchemaTable struct {
	Name    string         `json:"name"`
	Columns []SchemaColumn `json:"columns"`
}

type schemaResponse struct {
	Schema []SchemaTable `json:"schema"`
}

// QueryUser identifies the owner of a query or dashboard.
type QueryUser struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// QueryParameter is one declared parameter of a saved query.
type QueryParameter struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	Value       any    `json:"value"`
	EnumOptions string `json:"enumOptions"`
}

// QueryOptions is the options blob of a saved query.
type QueryOptions struct {
	Parameters []QueryParameter `json:"parameters"`
}

// Query is a saved Redash query.
type Query struct {
	ID                int          `json:"id"`
	Name              string       `json:"name"`
	Description       string       `json:"description"`
	Query             string       `json:"query"`
	DataSourceID      int          `json:"data_source_id"`
	IsArchived        bool         `json:"is_archived"`
	IsDraft           bool         `json:"is_draft"`
	Tags              []string     `json:"tags"`
	Options           QueryOptions `json:"options"`
	LatestQueryDataID *int         `json:"latest_query_data_id"`
	CreatedAt         string       `json:"created_at"`
	UpdatedAt         string       `json:"updated_at"`
	User              QueryUser    `json:"user"`
}

// Page is Redash's paginated list envelope.
type Page[T any] struct {
	Count    int `json:"count"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Results  []T `json:"results"`
}

// Job states reported by GET /api/jobs/{id}.
const (
	JobPending   = 1
	JobStarted   = 2
	JobFinished  = 3
	JobFailed    = 4
	JobCancelled = 5
)

// Job is an asynchronous query-execution handle.
type Job struct {
	ID     string `json:"id"`
	Status int    `json:"status"`
	Error  string `json:"error"`
	// Redash versions disagree on the name of the result-id field, so both
	// spellings are decoded.
	QueryResultID *int `json:"query_result_id"`
	ResultID      *int `json:"result_id"`
}

// resultID returns the id of the produced query result, when reported.
func (j *Job) resultID() (int, bool) {
	if j.QueryResultID != nil && *j.QueryResultID != 0 {
		return *j.QueryResultID, true
	}
	if j.ResultID != nil && *j.ResultID != 0 {
		return *j.ResultID, true
	}
	return 0, false
}

// ResultColumn describes one column of a query result.
type ResultColumn struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	FriendlyName string `json:"friendly_name"`
}

// ResultData holds the rows of a query result.
type ResultData struct {
	Columns []ResultColumn   `json:"columns"`
	Rows    []map[string]any `json:"rows"`
}

// UnmarshalJSON tolerates Redash deployments that store result data as a
// JSON string rather than an embedded object.
func (r *ResultData) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		data = []byte(s)
	}
	type plain ResultData
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*r = ResultData(p)
	return nil
}

// QueryResult is a stored result of a query execution.
type QueryResult struct {
	ID           int        `json:"id"`
	QueryHash    string     `json:"query_hash"`
	DataSourceID int        `json:"data_source_id"`
	Data         ResultData `json:"data"`
	Runtime      float64    `json:"runtime"`
	RetrievedAt  string     `json:"retrieved_at"`
}

type queryResultResponse struct {
	QueryResult QueryResult `json:"query_result"`
}

type jobResponse struct {
	Job Job `json:"job"`
}

// executeResponse is the polymorphic response of execution endpoints: an
// immediately available (cached) result, or a job to poll.
type executeResponse struct {
	QueryResult *QueryResult `json:"query_result"`
	Job         *Job         `json:"job"`
}

// Session is the response of GET /api/session.
type Session struct {
	OrgSlug string      `json:"org_slug"`
	User    SessionUser `json:"user"`
}

// SessionUser is the authenticated user.
type SessionUser struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Dashboard is a Redash dashboard; Widgets is populated only by the detail
// endpoint.
type Dashboard struct {
	ID         int       `json:"id"`
	Slug       string    `json:"slug"`
	Name       string    `json:"name"`
	Tags       []string  `json:"tags"`
	IsArchived bool      `json:"is_archived"`
	IsDraft    bool      `json:"is_draft"`
	UpdatedAt  string    `json:"updated_at"`
	User       QueryUser `json:"user"`
	Widgets    []Widget  `json:"widgets"`
}

// Widget is one dashboard tile: free text, or a visualization of a query.
type Widget struct {
	ID            int            `json:"id"`
	Text          string         `json:"text"`
	Visualization *Visualization `json:"visualization"`
}

// Visualization links a widget to its underlying query.
type Visualization struct {
	ID    int    `json:"id"`
	Type  string `json:"type"`
	Name  string `json:"name"`
	Query *Query `json:"query"`
}
