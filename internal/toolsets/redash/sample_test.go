package redashtools

import (
	"reflect"
	"testing"

	"mew/internal/redash"
)

func TestCollectFieldPaths(t *testing.T) {
	columns := []redash.ResultColumn{{Name: "_id"}, {Name: "email"}}
	rows := []map[string]any{
		{
			"_id":          "1",
			"email":        "a@b.com",
			"phone_number": "123",
			"address":      map[string]any{"city": "Pune", "geo": map[string]any{"lat": 1.0}},
			"tags":         []any{"x", "y"},
			"contacts": []any{
				map[string]any{"type": "home", "value": "555"},
			},
		},
		{
			"_id":   "2",
			"email": "c@d.com",
			"extra": "only-in-second",
		},
	}

	got := collectFieldPaths(columns, rows)
	want := []string{
		"_id",
		"address.city",
		"address.geo.lat",
		"contacts",
		"contacts[].type",
		"contacts[].value",
		"email",
		"extra",
		"phone_number",
		"tags",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("collectFieldPaths()\n got: %v\nwant: %v", got, want)
	}
}

func TestCollectFieldPathsEmptyNestedDoc(t *testing.T) {
	rows := []map[string]any{{"meta": map[string]any{}}}
	got := collectFieldPaths(nil, rows)
	want := []string{"meta"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestCollectFieldPathsDepthCap(t *testing.T) {
	// Build a document nested deeper than maxSampleDepth and confirm we stop
	// descending without panicking.
	deep := map[string]any{"v": "leaf"}
	for range maxSampleDepth + 3 {
		deep = map[string]any{"n": deep}
	}
	got := collectFieldPaths(nil, []map[string]any{deep})
	if len(got) == 0 {
		t.Fatal("expected some paths from a deep document")
	}
	for _, p := range got {
		if len(p) > 200 {
			t.Fatalf("path unexpectedly long, recursion not bounded: %q", p)
		}
	}
}
