package redashtools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mew/internal/redash"
)

type getSchemaArgs struct {
	DataSourceID int    `json:"data_source_id" jsonschema:"ID of the data source, from redash_list_data_sources"`
	Search       string `json:"search,omitempty" jsonschema:"Case-insensitive substring filter on collection names"`
	Limit        int    `json:"limit,omitempty" jsonschema:"Maximum collections to return (default 50, max 500)"`
}

func registerDataSourceTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "redash_test_connection",
		Description: "Verify that the Redash base URL and API key work. Returns the authenticated user and how many data sources are visible. Run this first when other Redash tools fail.",
		Annotations: readOnly("Test Redash connection"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		sess, err := d.Client.Session(ctx)
		if err != nil {
			return nil, nil, userErr(err)
		}
		sources, err := d.Client.DataSources(ctx)
		if err != nil {
			return nil, nil, userErr(err)
		}
		mongoCount := 0
		for _, ds := range sources {
			if isMongo(ds) {
				mongoCount++
			}
		}
		res, err := jsonResult(map[string]any{
			"status":               "ok",
			"base_url":             d.Config.BaseURL,
			"user":                 map[string]string{"name": sess.User.Name, "email": sess.User.Email},
			"org":                  sess.OrgSlug,
			"data_sources_visible": len(sources),
			"mongodb_data_sources": mongoCount,
		})
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "redash_list_data_sources",
		Description: "List the Redash data sources visible to the API key. Sources with is_mongodb=true accept JSON queries via redash_execute_adhoc_query.",
		Annotations: readOnly("List Redash data sources"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		sources, err := d.Client.DataSources(ctx)
		if err != nil {
			return nil, nil, userErr(err)
		}
		type dsView struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Type        string `json:"type"`
			IsMongoDB   bool   `json:"is_mongodb"`
			Paused      bool   `json:"paused,omitempty"`
			PauseReason string `json:"pause_reason,omitempty"`
			ViewOnly    bool   `json:"view_only,omitempty"`
		}
		views := make([]dsView, 0, len(sources))
		for _, ds := range sources {
			views = append(views, dsView{
				ID:          ds.ID,
				Name:        ds.Name,
				Type:        ds.Type,
				IsMongoDB:   isMongo(ds),
				Paused:      bool(ds.Paused),
				PauseReason: ds.PauseReason,
				ViewOnly:    ds.ViewOnly,
			})
		}
		res, err := jsonResult(map[string]any{"count": len(views), "data_sources": views})
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "redash_get_schema",
		Description: "Get the cached schema of a data source: its collections and the field names Redash has observed (dotted paths for nested documents). Use search to find specific collections. The schema is a sampled cache and may lag reality.",
		Annotations: readOnly("Get data source schema"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getSchemaArgs) (*mcp.CallToolResult, any, error) {
		tables, err := d.Client.Schema(ctx, args.DataSourceID)
		if err != nil {
			return nil, nil, userErr(err)
		}
		total := len(tables)

		if args.Search != "" {
			needle := strings.ToLower(args.Search)
			var filtered []redash.SchemaTable
			for _, t := range tables {
				if strings.Contains(strings.ToLower(t.Name), needle) {
					filtered = append(filtered, t)
				}
			}
			tables = filtered
		}
		matched := len(tables)

		limit := args.Limit
		if limit <= 0 {
			limit = 50
		}
		if limit > 500 {
			limit = 500
		}
		truncated := false
		if len(tables) > limit {
			tables = tables[:limit]
			truncated = true
		}

		const maxFields = 200
		type collView struct {
			Name      string   `json:"name"`
			Fields    []string `json:"fields"`
			FieldNote string   `json:"field_note,omitempty"`
		}
		colls := make([]collView, 0, len(tables))
		for _, t := range tables {
			fields := make([]string, 0, len(t.Columns))
			for _, c := range t.Columns {
				name := c.Name
				if c.Type != "" {
					name += " (" + c.Type + ")"
				}
				fields = append(fields, name)
			}
			view := collView{Name: t.Name, Fields: fields}
			if len(fields) > maxFields {
				view.Fields = fields[:maxFields]
				view.FieldNote = fmt.Sprintf("%d more fields omitted", len(fields)-maxFields)
			}
			colls = append(colls, view)
		}

		out := map[string]any{
			"total_collections":   total,
			"matched_collections": matched,
			"returned":            len(colls),
			"collections":         colls,
		}
		if truncated {
			out["note"] = "More collections matched than returned; refine search or raise limit."
		}
		res, err := jsonResult(out)
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})
}
