// Package clickuptools registers the ClickUp toolset: MCP tools that let a
// model browse (and optionally update) the company ClickUp workspace.
package clickuptools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mew/internal/clickup"
	"mew/internal/config"
)

// Deps carries the shared dependencies of all ClickUp tools.
type Deps struct {
	Client *clickup.Client
	Config config.ClickUp
	Logger *slog.Logger
}

// Instructions describes this toolset to MCP clients; the server includes
// it in its initialize response.
func Instructions(writesEnabled bool) string {
	s := `## ClickUp
Tools prefixed clickup_ work against the company ClickUp workspace.
Hierarchy: workspace -> space -> folder -> list -> task.
Typical flows:
- Orient: clickup_test_connection -> clickup_list_spaces -> clickup_list_lists.
- Find work: clickup_search_tasks (workspace-wide) or clickup_list_tasks (one list).
- Inspect: clickup_get_task -> clickup_get_comments.
Dates are ISO 8601 in both arguments and results. When one workspace is
visible, team_id can be omitted everywhere.`
	if writesEnabled {
		s += `
Write tools (clickup_create_task, clickup_update_task, clickup_add_comment)
make changes visible to the whole team, attributed to the token's user:
confirm intent before mutating, and prefer linking the returned task URL.`
	} else {
		s += `
This toolset is read-only: write tools are disabled by configuration.`
	}
	return s
}

// Register adds the ClickUp tools to s, honoring config toggles.
func Register(s *mcp.Server, d Deps) {
	registerWorkspaceTools(s, d)
	registerTaskTools(s, d)
	if d.Config.EnableWrites {
		registerWriteTools(s, d)
	}
}

func ptr[T any](v T) *T { return &v }

// readOnly marks browsing tools so clients may relax approval prompts.
func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true}
}

// writeAnnotations marks mutating tools; destructive says whether existing
// content can be overwritten (task updates) rather than only added.
func writeAnnotations(title string, destructive bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		Title:           title,
		DestructiveHint: ptr(destructive),
		OpenWorldHint:   ptr(true),
	}
}

// textResult wraps text as a tool result.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// jsonResult renders v as indented JSON text content.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	enc, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return nil, fmt.Errorf("internal: encoding tool result: %w", err)
	}
	return textResult(string(enc)), nil
}

// userErr augments ClickUp API errors with remediation hints.
func userErr(err error) error {
	var apiErr *clickup.APIError
	if errors.As(err, &apiErr) {
		if hint := apiErr.Hint(); hint != "" {
			return fmt.Errorf("%w (%s)", apiErr, hint)
		}
	}
	return err
}

// resolveTeam picks the workspace for a call: explicit argument, then the
// configured default, then the only visible workspace.
func resolveTeam(ctx context.Context, d Deps, arg string) (string, error) {
	if arg != "" {
		return arg, nil
	}
	if d.Config.TeamID != "" {
		return d.Config.TeamID, nil
	}
	teams, err := d.Client.Teams(ctx)
	if err != nil {
		return "", userErr(err)
	}
	if len(teams) == 1 {
		return teams[0].ID, nil
	}
	return "", fmt.Errorf("multiple ClickUp workspaces are visible; pass team_id (see clickup_test_connection) or set %s", config.EnvClickUpTeamID)
}

// parseTimeArg accepts an ISO 8601 date ("2026-06-12") or datetime
// (RFC 3339) and returns epoch milliseconds.
func parseTimeArg(s string) (int64, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UnixMilli(), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UnixMilli(), nil
	}
	return 0, fmt.Errorf("invalid date %q: use YYYY-MM-DD or RFC 3339 (2026-06-12T15:00:00Z)", s)
}

// msToISO renders ClickUp's epoch-millisecond strings as UTC ISO 8601,
// passing unparseable values through untouched.
func msToISO(ms clickup.FlexString) string {
	if ms == "" {
		return ""
	}
	n, err := strconv.ParseInt(string(ms), 10, 64)
	if err != nil {
		return string(ms)
	}
	return time.UnixMilli(n).UTC().Format(time.RFC3339)
}

// clip shortens long strings for list views without splitting a UTF-8 rune.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
