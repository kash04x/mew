// Package redashtools registers the Redash toolset: MCP tools that let a
// model browse and query a Redash instance backed by production MongoDB.
package redashtools

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mew/internal/config"
	"mew/internal/redash"
	"mew/internal/redash/format"
)

// Deps carries the shared dependencies of all Redash tools.
type Deps struct {
	Client *redash.Client
	Config config.Redash
	Logger *slog.Logger
	Poll   redash.PollConfig
}

// Instructions describes this toolset to MCP clients; the server includes
// it in its initialize response.
const Instructions = `## Redash (production MongoDB)
Tools prefixed redash_ work against the company Redash instance, whose data
sources are production MongoDB databases queried with Redash's JSON syntax.
Typical flows:
- Explore data: redash_list_data_sources -> redash_get_schema -> redash_execute_adhoc_query.
- Reuse vetted queries: redash_list_queries -> redash_get_query (note required parameters) -> redash_execute_query.
- Dashboards: redash_list_dashboards -> redash_get_dashboard -> run the widgets' queries by id.
Results are truncated to row and character budgets; notes in each payload say when.
This is production data: filter narrowly, project only needed fields, and
prefer aggregations over pulling raw documents.`

// Register adds the Redash tools to s, honoring config toggles.
func Register(s *mcp.Server, d Deps) {
	registerDataSourceTools(s, d)
	registerQueryTools(s, d)
	registerExecuteTools(s, d)
	registerDashboardTools(s, d)
}

func ptr[T any](v T) *T { return &v }

// readOnly marks browsing tools so clients may relax approval prompts.
func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true}
}

// executeAnnotations deliberately leaves ReadOnlyHint false: although these
// tools only read data, they consume production-database resources, so MCP
// clients should keep their approval gate in front of them.
func executeAnnotations(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		Title:           title,
		DestructiveHint: ptr(false),
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

// userErr augments Redash API errors with remediation hints.
func userErr(err error) error {
	var apiErr *redash.APIError
	if errors.As(err, &apiErr) {
		if hint := apiErr.Hint(); hint != "" {
			return fmt.Errorf("%w (%s)", apiErr, hint)
		}
	}
	return err
}

// limitsFor combines a tool call's max_rows with the configured budgets.
func limitsFor(cfg config.Redash, maxRows int) format.Limits {
	rows := config.DefaultRowsPerCall
	if maxRows > 0 {
		rows = maxRows
	}
	if rows > cfg.MaxRows {
		rows = cfg.MaxRows
	}
	return format.Limits{MaxRows: rows, MaxChars: cfg.MaxResultChars}
}

// isMongo guesses whether a data source speaks the Mongo JSON dialect.
func isMongo(ds redash.DataSource) bool {
	return strings.Contains(strings.ToLower(ds.Type), "mongo")
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
