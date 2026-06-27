package redashtools

import "testing"

import "mew/internal/redash"

func sampleSources() []redash.DataSource {
	return []redash.DataSource{
		{ID: 1, Name: "SC PROD DB"},
		{ID: 2, Name: "Analytics Replica"},
	}
}

func TestFindDataSourceByName(t *testing.T) {
	src := sampleSources()
	if id, ok := findDataSourceByName(src, "sc prod db"); !ok || id != 1 {
		t.Errorf("case-insensitive match failed: id=%d ok=%v", id, ok)
	}
	if id, ok := findDataSourceByName(src, "  SC PROD DB  "); !ok || id != 1 {
		t.Errorf("trimmed match failed: id=%d ok=%v", id, ok)
	}
	if _, ok := findDataSourceByName(src, "nope"); ok {
		t.Error("expected no match for unknown name")
	}
}

func TestIsDefaultSource(t *testing.T) {
	prod := redash.DataSource{ID: 1, Name: "SC PROD DB"}
	other := redash.DataSource{ID: 2, Name: "Analytics Replica"}

	if !isDefaultSource(prod, "SC PROD DB") {
		t.Error("expected name match to be default")
	}
	if !isDefaultSource(prod, "1") {
		t.Error("expected numeric id match to be default")
	}
	if isDefaultSource(other, "SC PROD DB") {
		t.Error("non-matching source must not be default")
	}
	if isDefaultSource(prod, "") {
		t.Error("empty default config means no default")
	}
}

func TestDefaultDataSourceHint(t *testing.T) {
	if defaultDataSourceHint("") != "" {
		t.Error("no hint when default is unset")
	}
	if defaultDataSourceHint("  ") != "" {
		t.Error("whitespace default should yield no hint")
	}
	hint := defaultDataSourceHint("SC PROD DB")
	if hint == "" {
		t.Fatal("expected a hint when default is set")
	}
	if want := "\"SC PROD DB\""; !contains(hint, want) {
		t.Errorf("hint should name the default source, got %q", hint)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
