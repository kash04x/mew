package clickuptools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listSpacesArgs struct {
	TeamID string `json:"team_id,omitempty" jsonschema:"Workspace id; optional when one workspace is visible or a default is configured"`
}

type listListsArgs struct {
	SpaceID string `json:"space_id" jsonschema:"ID of the space, from clickup_list_spaces"`
}

func registerWorkspaceTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "clickup_test_connection",
		Description: "Verify that the ClickUp API token works. Returns the authenticated user and the visible workspaces with their ids. Run this first when other ClickUp tools fail.",
		Annotations: readOnly("Test ClickUp connection"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		user, err := d.Client.User(ctx)
		if err != nil {
			return nil, nil, userErr(err)
		}
		teams, err := d.Client.Teams(ctx)
		if err != nil {
			return nil, nil, userErr(err)
		}
		type teamView struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Members int    `json:"members"`
		}
		views := make([]teamView, 0, len(teams))
		for _, t := range teams {
			views = append(views, teamView{ID: t.ID, Name: t.Name, Members: len(t.Members)})
		}
		res, err := jsonResult(map[string]any{
			"status":         "ok",
			"user":           map[string]string{"id": string(user.ID), "username": user.Username, "email": user.Email},
			"workspaces":     views,
			"writes_enabled": d.Config.EnableWrites,
		})
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clickup_list_spaces",
		Description: "List the spaces of a workspace, including each space's available task statuses (needed for status filters and task updates).",
		Annotations: readOnly("List ClickUp spaces"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args listSpacesArgs) (*mcp.CallToolResult, any, error) {
		teamID, err := resolveTeam(ctx, d, args.TeamID)
		if err != nil {
			return nil, nil, err
		}
		spaces, err := d.Client.Spaces(ctx, teamID)
		if err != nil {
			return nil, nil, userErr(err)
		}
		type spaceView struct {
			ID       string   `json:"id"`
			Name     string   `json:"name"`
			Private  bool     `json:"private,omitempty"`
			Statuses []string `json:"statuses,omitempty"`
		}
		views := make([]spaceView, 0, len(spaces))
		for _, sp := range spaces {
			statuses := make([]string, 0, len(sp.Statuses))
			for _, st := range sp.Statuses {
				statuses = append(statuses, st.Status)
			}
			views = append(views, spaceView{ID: sp.ID, Name: sp.Name, Private: sp.Private, Statuses: statuses})
		}
		res, err := jsonResult(map[string]any{"team_id": teamID, "count": len(views), "spaces": views})
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "clickup_list_lists",
		Description: "List the folders and lists of a space. Lists are where tasks live; use the list ids with clickup_list_tasks.",
		Annotations: readOnly("List ClickUp lists"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args listListsArgs) (*mcp.CallToolResult, any, error) {
		folders, err := d.Client.Folders(ctx, args.SpaceID)
		if err != nil {
			return nil, nil, userErr(err)
		}
		folderless, err := d.Client.FolderlessLists(ctx, args.SpaceID)
		if err != nil {
			return nil, nil, userErr(err)
		}

		type listView struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		type folderView struct {
			ID    string     `json:"id"`
			Name  string     `json:"name"`
			Lists []listView `json:"lists"`
		}
		folderViews := make([]folderView, 0, len(folders))
		listCount := 0
		for _, f := range folders {
			lists := make([]listView, 0, len(f.Lists))
			for _, l := range f.Lists {
				if l.Archived {
					continue
				}
				lists = append(lists, listView{ID: l.ID, Name: l.Name})
			}
			listCount += len(lists)
			folderViews = append(folderViews, folderView{ID: f.ID, Name: f.Name, Lists: lists})
		}
		rootLists := make([]listView, 0, len(folderless))
		for _, l := range folderless {
			if l.Archived {
				continue
			}
			rootLists = append(rootLists, listView{ID: l.ID, Name: l.Name})
		}
		res, err := jsonResult(map[string]any{
			"space_id":         args.SpaceID,
			"total_lists":      listCount + len(rootLists),
			"folders":          folderViews,
			"folderless_lists": rootLists,
		})
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})
}
