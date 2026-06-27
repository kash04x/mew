package clickuptools

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mew/internal/clickup"
)

type listTasksArgs struct {
	ListID        string   `json:"list_id" jsonschema:"ID of the list, from clickup_list_lists"`
	Statuses      []string `json:"statuses,omitempty" jsonschema:"Only tasks in these statuses (names from clickup_list_spaces)"`
	AssigneeIDs   []string `json:"assignee_ids,omitempty" jsonschema:"Only tasks assigned to these user ids"`
	Tags          []string `json:"tags,omitempty" jsonschema:"Only tasks carrying these tags"`
	IncludeClosed bool     `json:"include_closed,omitempty" jsonschema:"Include closed/done tasks (default false)"`
	DueBefore     string   `json:"due_before,omitempty" jsonschema:"Only tasks due before this date (YYYY-MM-DD or RFC 3339)"`
	DueAfter      string   `json:"due_after,omitempty" jsonschema:"Only tasks due after this date (YYYY-MM-DD or RFC 3339)"`
	Page          int      `json:"page,omitempty" jsonschema:"Page number starting at 0; pages hold up to 100 tasks"`
}

type searchTasksArgs struct {
	TeamID        string   `json:"team_id,omitempty" jsonschema:"Workspace id; optional when one workspace is visible or a default is configured"`
	SpaceIDs      []string `json:"space_ids,omitempty" jsonschema:"Restrict to these spaces"`
	ListIDs       []string `json:"list_ids,omitempty" jsonschema:"Restrict to these lists"`
	Statuses      []string `json:"statuses,omitempty" jsonschema:"Only tasks in these statuses (names from clickup_list_spaces)"`
	AssigneeIDs   []string `json:"assignee_ids,omitempty" jsonschema:"Only tasks assigned to these user ids"`
	Tags          []string `json:"tags,omitempty" jsonschema:"Only tasks carrying these tags"`
	IncludeClosed bool     `json:"include_closed,omitempty" jsonschema:"Include closed/done tasks (default false)"`
	DueBefore     string   `json:"due_before,omitempty" jsonschema:"Only tasks due before this date (YYYY-MM-DD or RFC 3339)"`
	DueAfter      string   `json:"due_after,omitempty" jsonschema:"Only tasks due after this date (YYYY-MM-DD or RFC 3339)"`
	Page          int      `json:"page,omitempty" jsonschema:"Page number starting at 0; pages hold up to 100 tasks"`
}

// buildFilters converts shared tool arguments into client filters.
func buildFilters(page int, statuses, assignees, tags []string, includeClosed bool, dueBefore, dueAfter string) (clickup.TaskFilters, error) {
	f := clickup.TaskFilters{
		Page:          page,
		Statuses:      statuses,
		AssigneeIDs:   assignees,
		Tags:          tags,
		IncludeClosed: includeClosed,
	}
	if dueBefore != "" {
		ms, err := parseTimeArg(dueBefore)
		if err != nil {
			return f, fmt.Errorf("due_before: %w", err)
		}
		f.DueBefore = ms
	}
	if dueAfter != "" {
		ms, err := parseTimeArg(dueAfter)
		if err != nil {
			return f, fmt.Errorf("due_after: %w", err)
		}
		f.DueAfter = ms
	}
	return f, nil
}

type getTaskArgs struct {
	TaskID          string `json:"task_id" jsonschema:"ID of the task (e.g. 86czkqfvb, from task lists or a ClickUp URL)"`
	IncludeSubtasks bool   `json:"include_subtasks,omitempty" jsonschema:"Also return the task's subtasks"`
}

type getCommentsArgs struct {
	TaskID string `json:"task_id" jsonschema:"ID of the task"`
}

// taskView is the compact task representation returned by list tools.
type taskView struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Status    string   `json:"status,omitempty"`
	Priority  string   `json:"priority,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	DueDate   string   `json:"due_date,omitempty"`
	List      string   `json:"list,omitempty"`
	Folder    string   `json:"folder,omitempty"`
	UpdatedAt string   `json:"updated_at,omitempty"`
	URL       string   `json:"url,omitempty"`
}

func toTaskView(t clickup.Task) taskView {
	v := taskView{
		ID:        t.ID,
		Name:      clip(t.Name, 200),
		Status:    t.Status.Status,
		DueDate:   msToISO(t.DueDate),
		UpdatedAt: msToISO(t.DateUpdated),
		URL:       t.URL,
	}
	if t.Priority != nil {
		v.Priority = t.Priority.Priority
	}
	for _, a := range t.Assignees {
		v.Assignees = append(v.Assignees, a.Username)
	}
	for _, tag := range t.Tags {
		v.Tags = append(v.Tags, tag.Name)
	}
	if t.List != nil {
		v.List = t.List.Name
	}
	if t.Folder != nil {
		v.Folder = t.Folder.Name
	}
	return v
}

// budgetTasks trims views until the encoded payload fits maxChars, returning
// how many were kept.
func budgetTasks(views []taskView, maxChars int) []taskView {
	for len(views) > 1 {
		enc, err := json.Marshal(views)
		if err != nil || len(enc) <= maxChars {
			return views
		}
		views = views[:len(views)*3/4]
	}
	return views
}

func taskPageResult(d Deps, page *clickup.TasksPage, extra map[string]any) (*mcp.CallToolResult, error) {
	views := make([]taskView, 0, len(page.Tasks))
	for _, t := range page.Tasks {
		views = append(views, toTaskView(t))
	}
	kept := budgetTasks(views, d.Config.MaxResultChars)

	out := map[string]any{
		"returned":  len(kept),
		"last_page": page.LastPage,
		"tasks":     kept,
	}
	if len(kept) < len(views) {
		out["note"] = fmt.Sprintf("%d tasks dropped to fit the character budget; filter narrower or page", len(views)-len(kept))
	} else if !page.LastPage {
		out["note"] = "more tasks exist; request the next page"
	}
	maps.Copy(out, extra)
	return jsonResult(out)
}

func registerTaskTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "clickup_list_tasks",
		Description: "List the tasks of one list, with optional status/assignee/tag/due-date filters. Returns compact task views; use clickup_get_task for full detail.",
		Annotations: readOnly("List ClickUp tasks"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args listTasksArgs) (*mcp.CallToolResult, any, error) {
		f, err := buildFilters(args.Page, args.Statuses, args.AssigneeIDs, args.Tags, args.IncludeClosed, args.DueBefore, args.DueAfter)
		if err != nil {
			return nil, nil, err
		}
		page, err := d.Client.ListTasks(ctx, args.ListID, f)
		if err != nil {
			return nil, nil, userErr(err)
		}
		res, err := taskPageResult(d, page, map[string]any{"list_id": args.ListID})
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clickup_search_tasks",
		Description: "Search tasks across a whole workspace by status, assignee, tag, due date, space or list. The broadest way to find tasks when the list is unknown.",
		Annotations: readOnly("Search ClickUp tasks"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args searchTasksArgs) (*mcp.CallToolResult, any, error) {
		teamID, err := resolveTeam(ctx, d, args.TeamID)
		if err != nil {
			return nil, nil, err
		}
		f, err := buildFilters(args.Page, args.Statuses, args.AssigneeIDs, args.Tags, args.IncludeClosed, args.DueBefore, args.DueAfter)
		if err != nil {
			return nil, nil, err
		}
		f.SpaceIDs = args.SpaceIDs
		f.ListIDs = args.ListIDs
		page, err := d.Client.TeamTasks(ctx, teamID, f)
		if err != nil {
			return nil, nil, userErr(err)
		}
		res, err := taskPageResult(d, page, map[string]any{"team_id": teamID})
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clickup_get_task",
		Description: "Fetch one task in full: description, assignees, dates, custom fields, and optionally subtasks.",
		Annotations: readOnly("Get ClickUp task"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getTaskArgs) (*mcp.CallToolResult, any, error) {
		t, err := d.Client.Task(ctx, args.TaskID, args.IncludeSubtasks)
		if err != nil {
			return nil, nil, userErr(err)
		}

		out := map[string]any{
			"task":        toTaskView(*t),
			"description": clip(t.Description, 4000),
			"created_at":  msToISO(t.DateCreated),
		}
		if t.CustomID != "" {
			out["custom_id"] = string(t.CustomID)
		}
		if t.Creator != nil {
			out["creator"] = t.Creator.Username
		}
		if t.Parent != "" {
			out["parent_task_id"] = string(t.Parent)
		}
		if t.StartDate != "" {
			out["start_date"] = msToISO(t.StartDate)
		}
		if t.DateClosed != "" {
			out["closed_at"] = msToISO(t.DateClosed)
		}
		if t.TimeEstimate != nil && *t.TimeEstimate > 0 {
			out["time_estimate_hours"] = float64(*t.TimeEstimate) / 3_600_000
		}
		if len(t.CustomFields) > 0 {
			type fieldView struct {
				Name  string `json:"name"`
				Type  string `json:"type"`
				Value string `json:"value,omitempty"`
			}
			fields := make([]fieldView, 0, len(t.CustomFields))
			for _, f := range t.CustomFields {
				v := fieldView{Name: f.Name, Type: f.Type}
				if len(f.Value) > 0 && string(f.Value) != "null" {
					v.Value = clip(string(f.Value), 300)
				}
				fields = append(fields, v)
			}
			out["custom_fields"] = fields
		}
		if len(t.Subtasks) > 0 {
			subs := make([]taskView, 0, len(t.Subtasks))
			for _, st := range t.Subtasks {
				subs = append(subs, toTaskView(st))
			}
			out["subtasks"] = subs
		}
		res, err := jsonResult(out)
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clickup_get_comments",
		Description: "Read a task's comments, newest first (ClickUp returns up to the latest 25).",
		Annotations: readOnly("Get ClickUp task comments"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getCommentsArgs) (*mcp.CallToolResult, any, error) {
		comments, err := d.Client.Comments(ctx, args.TaskID)
		if err != nil {
			return nil, nil, userErr(err)
		}
		type commentView struct {
			ID       string `json:"id"`
			Author   string `json:"author"`
			Date     string `json:"date"`
			Text     string `json:"text"`
			Resolved bool   `json:"resolved,omitempty"`
		}
		views := make([]commentView, 0, len(comments))
		for _, c := range comments {
			views = append(views, commentView{
				ID:       string(c.ID),
				Author:   c.User.Username,
				Date:     msToISO(c.Date),
				Text:     clip(c.CommentText, 1500),
				Resolved: c.Resolved,
			})
		}
		out := map[string]any{"task_id": args.TaskID, "count": len(views), "comments": views}
		if len(views) == 25 {
			out["note"] = "showing the newest 25 comments; older comments may exist"
		}
		res, err := jsonResult(out)
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})
}
