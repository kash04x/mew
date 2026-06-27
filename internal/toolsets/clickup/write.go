package clickuptools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mew/internal/clickup"
)

type createTaskArgs struct {
	ListID      string   `json:"list_id" jsonschema:"ID of the list to create the task in"`
	Name        string   `json:"name" jsonschema:"Task title"`
	Description string   `json:"description,omitempty" jsonschema:"Task description, markdown supported"`
	AssigneeIDs []int64  `json:"assignee_ids,omitempty" jsonschema:"Numeric user ids to assign (from clickup_test_connection or task views)"`
	Status      string   `json:"status,omitempty" jsonschema:"Initial status name; defaults to the list's first status"`
	Priority    int      `json:"priority,omitempty" jsonschema:"Priority where 1=urgent, 2=high, 3=normal, 4=low"`
	DueDate     string   `json:"due_date,omitempty" jsonschema:"Due date, YYYY-MM-DD or RFC 3339"`
	Tags        []string `json:"tags,omitempty" jsonschema:"Tags to attach"`
	ParentID    string   `json:"parent_id,omitempty" jsonschema:"Parent task id to create this as a subtask"`
}

type updateTaskArgs struct {
	TaskID            string  `json:"task_id" jsonschema:"ID of the task to update"`
	Name              string  `json:"name,omitempty" jsonschema:"New title; empty leaves it unchanged"`
	Description       string  `json:"description,omitempty" jsonschema:"New description; empty leaves it unchanged"`
	Status            string  `json:"status,omitempty" jsonschema:"New status name (must exist in the task's space)"`
	Priority          int     `json:"priority,omitempty" jsonschema:"Priority where 1=urgent, 2=high, 3=normal, 4=low; 0 leaves it unchanged"`
	DueDate           string  `json:"due_date,omitempty" jsonschema:"New due date, YYYY-MM-DD or RFC 3339; empty leaves it unchanged"`
	AddAssigneeIDs    []int64 `json:"add_assignee_ids,omitempty" jsonschema:"Numeric user ids to add as assignees"`
	RemoveAssigneeIDs []int64 `json:"remove_assignee_ids,omitempty" jsonschema:"Numeric user ids to remove from assignees"`
}

type addCommentArgs struct {
	TaskID    string `json:"task_id" jsonschema:"ID of the task to comment on"`
	Text      string `json:"comment_text" jsonschema:"Comment body"`
	NotifyAll bool   `json:"notify_all,omitempty" jsonschema:"Notify everyone on the task (default false)"`
}

func validPriority(p int) error {
	if p < 0 || p > 4 {
		return fmt.Errorf("priority must be 1 (urgent) to 4 (low), got %d", p)
	}
	return nil
}

func registerWriteTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "clickup_create_task",
		Description: "Create a task in a list. The task is immediately visible to the whole team and attributed to the API token's user, so confirm details before calling. Returns the new task with its URL.",
		Annotations: writeAnnotations("Create ClickUp task", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args createTaskArgs) (*mcp.CallToolResult, any, error) {
		if args.Name == "" {
			return nil, nil, fmt.Errorf("name is required")
		}
		if err := validPriority(args.Priority); err != nil {
			return nil, nil, err
		}
		req := clickup.CreateTaskRequest{
			Name:                args.Name,
			MarkdownDescription: args.Description,
			Assignees:           args.AssigneeIDs,
			Tags:                args.Tags,
			Status:              args.Status,
			Priority:            args.Priority,
			Parent:              args.ParentID,
		}
		if args.DueDate != "" {
			ms, err := parseTimeArg(args.DueDate)
			if err != nil {
				return nil, nil, fmt.Errorf("due_date: %w", err)
			}
			req.DueDate = ms
		}
		t, err := d.Client.CreateTask(ctx, args.ListID, req)
		if err != nil {
			return nil, nil, userErr(err)
		}
		res, err := jsonResult(map[string]any{"created": true, "task": toTaskView(*t)})
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clickup_update_task",
		Description: "Update fields of a task: title, description, status, priority, due date, assignees. Only the provided fields change, but provided fields overwrite the current values — fetch the task first when editing text.",
		Annotations: writeAnnotations("Update ClickUp task", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args updateTaskArgs) (*mcp.CallToolResult, any, error) {
		if err := validPriority(args.Priority); err != nil {
			return nil, nil, err
		}
		var req clickup.UpdateTaskRequest
		changes := 0
		if args.Name != "" {
			req.Name = &args.Name
			changes++
		}
		if args.Description != "" {
			req.Description = &args.Description
			changes++
		}
		if args.Status != "" {
			req.Status = &args.Status
			changes++
		}
		if args.Priority != 0 {
			req.Priority = &args.Priority
			changes++
		}
		if args.DueDate != "" {
			ms, err := parseTimeArg(args.DueDate)
			if err != nil {
				return nil, nil, fmt.Errorf("due_date: %w", err)
			}
			req.DueDate = &ms
			changes++
		}
		if len(args.AddAssigneeIDs) > 0 || len(args.RemoveAssigneeIDs) > 0 {
			req.Assignees = &clickup.AssigneeChanges{Add: args.AddAssigneeIDs, Rem: args.RemoveAssigneeIDs}
			changes++
		}
		if changes == 0 {
			return nil, nil, fmt.Errorf("nothing to update: provide at least one field")
		}
		t, err := d.Client.UpdateTask(ctx, args.TaskID, req)
		if err != nil {
			return nil, nil, userErr(err)
		}
		res, err := jsonResult(map[string]any{"updated": true, "task": toTaskView(*t)})
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clickup_add_comment",
		Description: "Post a comment on a task, attributed to the API token's user and visible to the whole team. Confirm wording before calling.",
		Annotations: writeAnnotations("Add ClickUp comment", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args addCommentArgs) (*mcp.CallToolResult, any, error) {
		if args.Text == "" {
			return nil, nil, fmt.Errorf("comment_text is required")
		}
		id, err := d.Client.AddComment(ctx, args.TaskID, args.Text, args.NotifyAll)
		if err != nil {
			return nil, nil, userErr(err)
		}
		res, err := jsonResult(map[string]any{"posted": true, "comment_id": id, "task_id": args.TaskID})
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})
}
