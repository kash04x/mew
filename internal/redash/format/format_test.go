package format

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"mew/internal/redash"
)

func render(t *testing.T, qr *redash.QueryResult, lim Limits, notes ...string) Result {
	t.Helper()
	text, err := QueryResult(qr, lim, notes...)
	if err != nil {
		t.Fatalf("QueryResult: %v", err)
	}
	var out Result
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	return out
}

func intRows(n int) []map[string]any {
	rows := make([]map[string]any, n)
	for i := range rows {
		rows[i] = map[string]any{"n": float64(i)}
	}
	return rows
}

func TestRowCap(t *testing.T) {
	qr := &redash.QueryResult{Data: redash.ResultData{
		Columns: []redash.ResultColumn{{Name: "n", Type: "integer"}},
		Rows:    intRows(10),
	}}
	out := render(t, qr, Limits{MaxRows: 3})
	if out.TotalRows != 10 || out.ReturnedRows != 3 || len(out.Rows) != 3 {
		t.Fatalf("rows: total=%d returned=%d len=%d", out.TotalRows, out.ReturnedRows, len(out.Rows))
	}
	if !out.Truncated || len(out.Notes) == 0 {
		t.Error("truncation should be flagged with a note")
	}
	if out.Columns[0].Name != "n" || out.Columns[0].Type != "integer" {
		t.Errorf("columns = %+v", out.Columns)
	}
}

func TestCharBudgetDropsRows(t *testing.T) {
	rows := make([]map[string]any, 20)
	for i := range rows {
		rows[i] = map[string]any{"text": strings.Repeat("x", 100)}
	}
	qr := &redash.QueryResult{Data: redash.ResultData{Rows: rows}}
	out := render(t, qr, Limits{MaxRows: 100, MaxChars: 600})
	if out.ReturnedRows == 0 || out.ReturnedRows >= 20 {
		t.Fatalf("returned = %d, want a partial set", out.ReturnedRows)
	}
	if !out.Truncated {
		t.Error("char-budget truncation should be flagged")
	}
}

func TestCellTruncation(t *testing.T) {
	qr := &redash.QueryResult{Data: redash.ResultData{
		Rows: []map[string]any{{"blob": strings.Repeat("a", 5000)}},
	}}
	out := render(t, qr, Limits{})
	cell, ok := out.Rows[0]["blob"].(string)
	if !ok {
		t.Fatalf("cell type = %T", out.Rows[0]["blob"])
	}
	if len(cell) > 2100 || !strings.Contains(cell, "[truncated, 5000 chars total]") {
		t.Errorf("cell not truncated as expected (len %d)", len(cell))
	}
}

func TestCellTruncationUTF8Safe(t *testing.T) {
	qr := &redash.QueryResult{Data: redash.ResultData{
		Rows: []map[string]any{{"s": strings.Repeat("é", 3000)}},
	}}
	out := render(t, qr, Limits{MaxCellChars: 2001}) // odd cut point inside a 2-byte rune
	cell := out.Rows[0]["s"].(string)
	if !utf8.ValidString(cell) {
		t.Error("truncated cell contains invalid UTF-8")
	}
}

func TestNestedValueTruncatedAsJSON(t *testing.T) {
	big := map[string]any{}
	for i := range 300 {
		big[fmt.Sprintf("k%d", i)] = strings.Repeat("v", 20)
	}
	qr := &redash.QueryResult{Data: redash.ResultData{
		Rows: []map[string]any{{"doc": big}},
	}}
	out := render(t, qr, Limits{})
	cell, ok := out.Rows[0]["doc"].(string)
	if !ok || !strings.Contains(cell, "[truncated JSON") {
		t.Errorf("nested doc should be rendered as truncated JSON string, got %T", out.Rows[0]["doc"])
	}
}

func TestSmallNestedValueKeptStructured(t *testing.T) {
	qr := &redash.QueryResult{Data: redash.ResultData{
		Rows: []map[string]any{{"doc": map[string]any{"a": float64(1)}}},
	}}
	out := render(t, qr, Limits{})
	if _, ok := out.Rows[0]["doc"].(map[string]any); !ok {
		t.Errorf("small nested doc should stay structured, got %T", out.Rows[0]["doc"])
	}
}

func TestNotesAndMetadata(t *testing.T) {
	qr := &redash.QueryResult{
		Runtime:     1.23456,
		RetrievedAt: "2026-06-12T10:00:00Z",
		Data:        redash.ResultData{Rows: intRows(1)},
	}
	out := render(t, qr, Limits{}, "limit 1000 was injected")
	if len(out.Notes) != 1 || out.Notes[0] != "limit 1000 was injected" {
		t.Errorf("notes = %v", out.Notes)
	}
	if out.RuntimeSeconds != 1.235 {
		t.Errorf("runtime = %v, want rounded 1.235", out.RuntimeSeconds)
	}
	if out.RetrievedAt != "2026-06-12T10:00:00Z" {
		t.Errorf("retrieved_at = %q", out.RetrievedAt)
	}
	if out.Truncated {
		t.Error("nothing was truncated")
	}
}

func TestEmptyResult(t *testing.T) {
	qr := &redash.QueryResult{Data: redash.ResultData{}}
	out := render(t, qr, Limits{})
	if out.TotalRows != 0 || out.ReturnedRows != 0 || out.Truncated {
		t.Errorf("unexpected: %+v", out)
	}
	if out.Rows == nil {
		t.Error("rows should serialize as [], not null")
	}
}
