package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var toolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Inspect the MCP tools mew exposes",
}

var toolsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the MCP tools, grouped by toolset",
	Args:  cobra.NoArgs,
	RunE:  runToolsList,
}

func init() {
	toolsCmd.AddCommand(toolsListCmd)
	rootCmd.AddCommand(toolsCmd)
}

// toolInfo mirrors a tool registered in internal/toolsets. The kinds match
// the server's MCP annotations: "read" tools are safe to browse with,
// "execute" tools spend production-database resources, and "write" tools make
// team-visible changes and register only when writes are enabled.
type toolInfo struct {
	name string
	kind string
	desc string
}

var toolCatalog = map[string][]toolInfo{
	"redash": {
		{"redash_test_connection", "read", "Verify URL/key; show user and visible data sources"},
		{"redash_list_data_sources", "read", "List data sources; flags MongoDB ones"},
		{"redash_get_schema", "read", "Collections + observed fields of a data source"},
		{"redash_sample_documents", "read", "Sample real documents to see exact field names"},
		{"redash_list_queries", "read", "Search saved queries"},
		{"redash_get_query", "read", "Full query text + declared parameters"},
		{"redash_get_cached_result", "read", "Latest cached result, zero database load"},
		{"redash_list_dashboards", "read", "Search dashboards"},
		{"redash_get_dashboard", "read", "Dashboard widgets with underlying query ids"},
		{"redash_execute_query", "execute", "Run a saved query (with parameters), return rows"},
		{"redash_execute_adhoc_query", "execute", "Run a new JSON Mongo query, return rows"},
	},
	"clickup": {
		{"clickup_test_connection", "read", "Verify token; show user and visible workspaces"},
		{"clickup_list_spaces", "read", "Spaces of a workspace, with task statuses"},
		{"clickup_list_lists", "read", "Folders and lists of a space"},
		{"clickup_list_tasks", "read", "Tasks of one list, with filters"},
		{"clickup_search_tasks", "read", "Workspace-wide task search"},
		{"clickup_get_task", "read", "One task in full: description, fields, subtasks"},
		{"clickup_get_comments", "read", "Newest comments of a task"},
		{"clickup_create_task", "write", "Create a task in a list"},
		{"clickup_update_task", "write", "Update task fields"},
		{"clickup_add_comment", "write", "Post a comment on a task"},
	},
}

func runToolsList(cmd *cobra.Command, _ []string) error {
	file, err := loadSettings()
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	for _, ts := range []string{"redash", "clickup"} {
		configured, _ := file.Configured(ts)
		state := "active"
		switch {
		case !configured:
			state = "not configured"
		case file.IsDisabled(ts):
			state = "disabled"
		}
		fmt.Fprintf(tw, "\n%s\t(%s)\n", ts, state)
		writesOn, _ := file.Get(ts + ".enable_writes")
		for _, t := range toolCatalog[ts] {
			note := ""
			if t.kind == "write" && writesOn != "true" {
				note = "  — off (set " + ts + ".enable_writes=true)"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s%s\n", t.name, t.kind, t.desc, note)
		}
	}
	return tw.Flush()
}
