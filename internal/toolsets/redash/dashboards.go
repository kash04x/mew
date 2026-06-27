package redashtools

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mew/internal/redash"
)

type listDashboardsArgs struct {
	Search   string `json:"search,omitempty" jsonschema:"Search dashboards by name"`
	Page     int    `json:"page,omitempty" jsonschema:"Page number starting at 1 (default 1)"`
	PageSize int    `json:"page_size,omitempty" jsonschema:"Results per page, 1-100 (default 25)"`
}

type getDashboardArgs struct {
	Dashboard string `json:"dashboard" jsonschema:"Dashboard ID (preferred) or slug"`
}

func registerDashboardTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "redash_list_dashboards",
		Description: "List or search Redash dashboards. Returns ids and slugs for redash_get_dashboard.",
		Annotations: readOnly("List dashboards"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args listDashboardsArgs) (*mcp.CallToolResult, any, error) {
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
		result, err := d.Client.Dashboards(ctx, redash.DashboardsParams{
			Search: args.Search, Page: page, PageSize: size,
		})
		if err != nil {
			return nil, nil, userErr(err)
		}

		type dashView struct {
			ID         int      `json:"id"`
			Slug       string   `json:"slug,omitempty"`
			Name       string   `json:"name"`
			Tags       []string `json:"tags,omitempty"`
			IsDraft    bool     `json:"is_draft,omitempty"`
			IsArchived bool     `json:"is_archived,omitempty"`
			UpdatedAt  string   `json:"updated_at"`
			Author     string   `json:"author,omitempty"`
		}
		views := make([]dashView, 0, len(result.Results))
		for _, dash := range result.Results {
			views = append(views, dashView{
				ID:         dash.ID,
				Slug:       dash.Slug,
				Name:       dash.Name,
				Tags:       dash.Tags,
				IsDraft:    dash.IsDraft,
				IsArchived: dash.IsArchived,
				UpdatedAt:  dash.UpdatedAt,
				Author:     dash.User.Name,
			})
		}
		res, err := jsonResult(map[string]any{
			"total":      result.Count,
			"page":       result.Page,
			"page_size":  result.PageSize,
			"dashboards": views,
		})
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "redash_get_dashboard",
		Description: "Fetch a dashboard and its widgets, including each widget's underlying saved query id. Use redash_get_query / redash_execute_query on those ids to inspect or refresh the numbers.",
		Annotations: readOnly("Get dashboard"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getDashboardArgs) (*mcp.CallToolResult, any, error) {
		ref := strings.TrimSpace(args.Dashboard)
		if ref == "" {
			return nil, nil, errors.New("dashboard must be a dashboard id or slug")
		}
		dash, err := d.Client.Dashboard(ctx, ref)
		if err != nil {
			return nil, nil, userErr(err)
		}

		type widgetView struct {
			Kind      string `json:"kind"`
			Name      string `json:"name,omitempty"`
			VizType   string `json:"visualization_type,omitempty"`
			QueryID   int    `json:"query_id,omitempty"`
			QueryName string `json:"query_name,omitempty"`
			Text      string `json:"text,omitempty"`
		}
		widgets := make([]widgetView, 0, len(dash.Widgets))
		for _, w := range dash.Widgets {
			switch {
			case w.Visualization != nil:
				wv := widgetView{Kind: "visualization", Name: w.Visualization.Name, VizType: w.Visualization.Type}
				if w.Visualization.Query != nil {
					wv.QueryID = w.Visualization.Query.ID
					wv.QueryName = w.Visualization.Query.Name
				}
				widgets = append(widgets, wv)
			case strings.TrimSpace(w.Text) != "":
				widgets = append(widgets, widgetView{Kind: "text", Text: clip(w.Text, 500)})
			}
		}

		res, err := jsonResult(map[string]any{
			"id":         dash.ID,
			"slug":       dash.Slug,
			"name":       dash.Name,
			"tags":       dash.Tags,
			"updated_at": dash.UpdatedAt,
			"widgets":    widgets,
		})
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})
}
