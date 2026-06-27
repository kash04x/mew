// Package format renders Redash query results for model consumption,
// enforcing row, cell, and total character budgets so that one tool call
// can never flood the context window.
package format

import (
	"encoding/json"
	"fmt"
	"math"
	"unicode/utf8"

	"mew/internal/redash"
)

// Limits bounds how much of a result is rendered.
type Limits struct {
	// MaxRows caps the number of rows included. Defaults to 100.
	MaxRows int
	// MaxChars caps the total size spent on rows; once exceeded, remaining
	// rows are dropped. Defaults to 50000.
	MaxChars int
	// MaxCellChars caps each cell's rendering. Defaults to 2000.
	MaxCellChars int
}

const (
	defaultMaxRows      = 100
	defaultMaxChars     = 50_000
	defaultMaxCellChars = 2000
)

// Column is a result column in the rendered payload.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

// Result is the JSON document handed back to the model.
type Result struct {
	Columns        []Column         `json:"columns,omitempty"`
	TotalRows      int              `json:"total_rows"`
	ReturnedRows   int              `json:"returned_rows"`
	Truncated      bool             `json:"truncated,omitempty"`
	Notes          []string         `json:"notes,omitempty"`
	RuntimeSeconds float64          `json:"runtime_seconds,omitempty"`
	RetrievedAt    string           `json:"retrieved_at,omitempty"`
	Rows           []map[string]any `json:"rows"`
}

// QueryResult renders qr within lim and returns indented JSON text.
func QueryResult(qr *redash.QueryResult, lim Limits, extraNotes ...string) (string, error) {
	if lim.MaxRows <= 0 {
		lim.MaxRows = defaultMaxRows
	}
	if lim.MaxChars <= 0 {
		lim.MaxChars = defaultMaxChars
	}
	if lim.MaxCellChars <= 0 {
		lim.MaxCellChars = defaultMaxCellChars
	}

	out := Result{
		TotalRows:      len(qr.Data.Rows),
		RuntimeSeconds: math.Round(qr.Runtime*1000) / 1000,
		RetrievedAt:    qr.RetrievedAt,
		Notes:          append([]string(nil), extraNotes...),
		Rows:           []map[string]any{},
	}
	for _, c := range qr.Data.Columns {
		out.Columns = append(out.Columns, Column{Name: c.Name, Type: c.Type})
	}

	used := 0
	for _, row := range qr.Data.Rows {
		if len(out.Rows) >= lim.MaxRows {
			break
		}
		clean := sanitizeRow(row, lim.MaxCellChars)
		enc, err := json.Marshal(clean)
		if err != nil {
			clean = map[string]any{"_render_error": err.Error()}
			enc, _ = json.Marshal(clean)
		}
		if used+len(enc) > lim.MaxChars && len(out.Rows) > 0 {
			break
		}
		out.Rows = append(out.Rows, clean)
		used += len(enc)
	}

	out.ReturnedRows = len(out.Rows)
	if out.ReturnedRows < out.TotalRows {
		out.Truncated = true
		out.Notes = append(out.Notes, fmt.Sprintf(
			"Showing %d of %d rows. Narrow the query (filters, projection, aggregation) or pass a larger max_rows for more.",
			out.ReturnedRows, out.TotalRows))
	}

	enc, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		return "", fmt.Errorf("encoding query result: %w", err)
	}
	return string(enc), nil
}

func sanitizeRow(row map[string]any, maxCell int) map[string]any {
	clean := make(map[string]any, len(row))
	for k, v := range row {
		clean[k] = sanitizeValue(v, maxCell)
	}
	return clean
}

// sanitizeValue truncates oversized cells: long strings are cut directly,
// and large nested values are replaced by their truncated JSON rendering.
func sanitizeValue(v any, maxCell int) any {
	switch t := v.(type) {
	case nil, bool, float64, int, int64, json.Number:
		return v
	case string:
		if len(t) <= maxCell {
			return t
		}
		return cutString(t, maxCell) + fmt.Sprintf("… [truncated, %d chars total]", len(t))
	default:
		enc, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		if len(enc) <= maxCell {
			return v
		}
		return cutString(string(enc), maxCell) + fmt.Sprintf("… [truncated JSON, %d chars total]", len(enc))
	}
}

// cutString cuts s to at most max bytes without splitting a UTF-8 rune.
func cutString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
