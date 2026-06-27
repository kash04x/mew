package clickuptools

import (
	"encoding/json"
	"strings"
	"testing"

	"mew/internal/clickup"
)

func TestParseTimeArg(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{in: "2026-06-12", want: 1781222400000},
		{in: "2026-06-12T15:30:00Z", want: 1781278200000},
		{in: "tomorrow", wantErr: true},
		{in: "12/06/2026", wantErr: true},
	}
	for _, tc := range cases {
		got, err := parseTimeArg(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseTimeArg(%q): expected error, got %d", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseTimeArg(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseTimeArg(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestMsToISO(t *testing.T) {
	if got := msToISO("1781222400000"); got != "2026-06-12T00:00:00Z" {
		t.Errorf("msToISO = %q", got)
	}
	if got := msToISO(""); got != "" {
		t.Errorf("empty input should stay empty, got %q", got)
	}
	if got := msToISO("not-a-number"); got != "not-a-number" {
		t.Errorf("unparseable input should pass through, got %q", got)
	}
}

func TestToTaskView(t *testing.T) {
	task := clickup.Task{
		ID:          "86abc",
		Name:        "Ship it",
		Status:      clickup.TaskStatus{Status: "in progress"},
		Priority:    &clickup.TaskPriority{Priority: "high"},
		Assignees:   []clickup.User{{Username: "Akash"}},
		Tags:        []clickup.Tag{{Name: "backend"}},
		DueDate:     "1781222400000",
		DateUpdated: "1781222400000",
		List:        &clickup.NamedRef{ID: "l1", Name: "Backlog"},
		URL:         "https://app.clickup.com/t/86abc",
	}
	v := toTaskView(task)
	if v.Priority != "high" || v.List != "Backlog" || v.DueDate != "2026-06-12T00:00:00Z" {
		t.Errorf("unexpected view: %+v", v)
	}
	if len(v.Assignees) != 1 || v.Assignees[0] != "Akash" {
		t.Errorf("assignees: %v", v.Assignees)
	}
}

func TestBudgetTasksTrims(t *testing.T) {
	views := make([]taskView, 100)
	for i := range views {
		views[i] = taskView{ID: "task", Name: strings.Repeat("x", 100)}
	}
	full, _ := json.Marshal(views)
	kept := budgetTasks(views, len(full)/2)
	if len(kept) >= len(views) || len(kept) == 0 {
		t.Fatalf("kept %d of %d, want a non-empty strict subset", len(kept), len(views))
	}
	enc, _ := json.Marshal(kept)
	if len(enc) > len(full)/2 {
		t.Errorf("trimmed payload still %d bytes, budget %d", len(enc), len(full)/2)
	}
}

func TestBudgetTasksKeepsSmallPayloads(t *testing.T) {
	views := []taskView{{ID: "a"}, {ID: "b"}}
	if kept := budgetTasks(views, 50_000); len(kept) != 2 {
		t.Errorf("kept %d, want all", len(kept))
	}
}
