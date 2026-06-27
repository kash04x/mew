package mongoguard

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustParse(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("rewritten query is not valid JSON: %v\n%s", err, s)
	}
	return m
}

func TestPrepareRejections(t *testing.T) {
	cases := []struct {
		name, query, wantErr string
	}{
		{"empty", "   ", "query is empty"},
		{"not json", "db.users.find({})", "not a valid JSON object"},
		{"json array", `[{"collection":"x"}]`, "not a valid JSON object"},
		{"missing collection", `{"query":{}}`, `"collection"`},
		{"collection not string", `{"collection":5}`, `"collection"`},
		{"aggregate not array", `{"collection":"x","aggregate":{"$match":{}}}`, "array of pipeline stage"},
		{"out stage", `{"collection":"x","aggregate":[{"$match":{}},{"$out":"evil"}]}`, "$out"},
		{"merge stage", `{"collection":"x","aggregate":[{"$merge":{"into":"evil"}}]}`, "$merge"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Prepare(tc.query, 1000)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want mention of %q", err, tc.wantErr)
			}
		})
	}
}

func TestPrepareInjectsFindLimit(t *testing.T) {
	res, err := Prepare(`{"collection":"users","query":{"active":true}}`, 500)
	if err != nil {
		t.Fatal(err)
	}
	if res.InjectedLimit != 500 {
		t.Fatalf("InjectedLimit = %d, want 500", res.InjectedLimit)
	}
	m := mustParse(t, res.Query)
	if m["limit"] != float64(500) {
		t.Errorf("limit = %v, want 500", m["limit"])
	}
	if m["collection"] != "users" {
		t.Errorf("collection lost in rewrite: %v", m)
	}
	if q, ok := m["query"].(map[string]any); !ok || q["active"] != true {
		t.Errorf("query filter lost in rewrite: %v", m)
	}
}

func TestPrepareKeepsExplicitLimit(t *testing.T) {
	in := `{"collection":"users","limit":5}`
	res, err := Prepare(in, 500)
	if err != nil {
		t.Fatal(err)
	}
	if res.InjectedLimit != 0 || res.Query != in {
		t.Errorf("query with explicit limit was modified: %+v", res)
	}
}

func TestPrepareAutoLimitDisabled(t *testing.T) {
	in := `{"collection":"users"}`
	res, err := Prepare(in, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.InjectedLimit != 0 || res.Query != in {
		t.Errorf("query was modified with auto-limit disabled: %+v", res)
	}
}

func TestPrepareLeavesCountAlone(t *testing.T) {
	in := `{"collection":"users","query":{},"count":true}`
	res, err := Prepare(in, 500)
	if err != nil {
		t.Fatal(err)
	}
	if res.InjectedLimit != 0 || res.Query != in {
		t.Errorf("count query was modified: %+v", res)
	}
}

func TestPrepareAppendsAggregateLimit(t *testing.T) {
	res, err := Prepare(`{"collection":"orders","aggregate":[{"$match":{"status":"paid"}},{"$group":{"_id":"$c","n":{"$sum":1}}}]}`, 200)
	if err != nil {
		t.Fatal(err)
	}
	if res.InjectedLimit != 200 {
		t.Fatalf("InjectedLimit = %d, want 200", res.InjectedLimit)
	}
	m := mustParse(t, res.Query)
	stages, ok := m["aggregate"].([]any)
	if !ok || len(stages) != 3 {
		t.Fatalf("aggregate = %v, want 3 stages", m["aggregate"])
	}
	last, ok := stages[2].(map[string]any)
	if !ok || last["$limit"] != float64(200) {
		t.Errorf("last stage = %v, want {$limit: 200}", stages[2])
	}
}

func TestPrepareRespectsExistingPipelineLimit(t *testing.T) {
	in := `{"collection":"orders","aggregate":[{"$match":{}},{"$limit":10},{"$project":{"a":1}}]}`
	res, err := Prepare(in, 200)
	if err != nil {
		t.Fatal(err)
	}
	if res.InjectedLimit != 0 || res.Query != in {
		t.Errorf("pipeline with $limit was modified: %+v", res)
	}
}

func TestPrepareRespectsCountStage(t *testing.T) {
	in := `{"collection":"orders","aggregate":[{"$match":{}},{"$count":"total"}]}`
	res, err := Prepare(in, 200)
	if err != nil {
		t.Fatal(err)
	}
	if res.InjectedLimit != 0 || res.Query != in {
		t.Errorf("pipeline with $count was modified: %+v", res)
	}
}

func TestPrepareRejectsOutEvenWithLimit(t *testing.T) {
	_, err := Prepare(`{"collection":"x","aggregate":[{"$limit":5},{"$out":"evil"}]}`, 200)
	if err == nil || !strings.Contains(err.Error(), "$out") {
		t.Fatalf("err = %v, want $out rejection", err)
	}
}
