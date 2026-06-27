package redashtools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mew/internal/redash"
)

type listQueriesArgs struct {
	Search   string   `json:"search,omitempty" jsonschema:"Full-text search across query names and descriptions"`
	Tags     []string `json:"tags,omitempty" jsonschema:"Only queries carrying all of these tags"`
	Page     int      `json:"page,omitempty" jsonschema:"Page number starting at 1 (default 1)"`
	PageSize int      `json:"page_size,omitempty" jsonschema:"Results per page, 1-100 (default 25)"`
}

type getQueryArgs struct {
	QueryID int `json:"query_id" jsonschema:"ID of the saved Redash query"`
}

func registerQueryTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "redash_list_queries",
		Description: "List or search saved Redash queries (most recently updated first when no search is given). Returns ids for redash_get_query and redash_execute_query.",
		Annotations: readOnly("List saved queries"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args listQueriesArgs) (*mcp.CallToolResult, any, error) {
		page := args.Page
		if page < 1 {
			page = 1
		}
		size := args.PageSize
		if size <= 0 {
			size = 25
		}
		if size > 100 {
			size = 100
		}
		result, err := d.Client.Queries(ctx, redash.QueriesParams{
			Search: args.Search, Tags: args.Tags, Page: page, PageSize: size,
		})
		if err != nil {
			return nil, nil, userErr(err)
		}

		type queryView struct {
			ID           int      `json:"id"`
			Name         string   `json:"name"`
			Description  string   `json:"description,omitempty"`
			DataSourceID int      `json:"data_source_id"`
			Tags         []string `json:"tags,omitempty"`
			IsDraft      bool     `json:"is_draft,omitempty"`
			IsArchived   bool     `json:"is_archived,omitempty"`
			UpdatedAt    string   `json:"updated_at"`
			Author       string   `json:"author,omitempty"`
		}
		views := make([]queryView, 0, len(result.Results))
		for _, q := range result.Results {
			views = append(views, queryView{
				ID:           q.ID,
				Name:         q.Name,
				Description:  clip(q.Description, 200),
				DataSourceID: q.DataSourceID,
				Tags:         q.Tags,
				IsDraft:      q.IsDraft,
				IsArchived:   q.IsArchived,
				UpdatedAt:    q.UpdatedAt,
				Author:       q.User.Name,
			})
		}
		res, err := jsonResult(map[string]any{
			"total":     result.Count,
			"page":      result.Page,
			"page_size": result.PageSize,
			"queries":   views,
		})
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "redash_get_query",
		Description: "Fetch one saved query in full: its query text (Mongo JSON), data source, and declared parameters. Check parameters here before calling redash_execute_query.",
		Annotations: readOnly("Get saved query"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getQueryArgs) (*mcp.CallToolResult, any, error) {
		q, err := d.Client.QueryByID(ctx, args.QueryID)
		if err != nil {
			return nil, nil, userErr(err)
		}

		type paramView struct {
			Name        string `json:"name"`
			Title       string `json:"title,omitempty"`
			Type        string `json:"type,omitempty"`
			Default     any    `json:"default,omitempty"`
			EnumOptions string `json:"enum_options,omitempty"`
		}
		params := make([]paramView, 0, len(q.Options.Parameters))
		for _, p := range q.Options.Parameters {
			params = append(params, paramView{
				Name: p.Name, Title: p.Title, Type: p.Type,
				Default: p.Value, EnumOptions: p.EnumOptions,
			})
		}

		queryText := q.Query
		var note string
		if len(queryText) > 8000 {
			queryText = clip(queryText, 8000)
			note = "query text truncated"
		}

		out := map[string]any{
			"id":                q.ID,
			"name":              q.Name,
			"description":       q.Description,
			"data_source_id":    q.DataSourceID,
			"query":             queryText,
			"parameters":        params,
			"tags":              q.Tags,
			"is_draft":          q.IsDraft,
			"is_archived":       q.IsArchived,
			"has_cached_result": q.LatestQueryDataID != nil,
			"updated_at":        q.UpdatedAt,
			"author":            q.User.Name,
		}
		if note != "" {
			out["note"] = note
		}
		res, err := jsonResult(out)
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})
}
