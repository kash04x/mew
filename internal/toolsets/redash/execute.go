package redashtools

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mew/internal/redash"
	"mew/internal/redash/format"
	"mew/internal/redash/mongoguard"
)

const mongoQueryGuide = `The query must be one JSON object in Redash's Mongo syntax (strict JSON: no JavaScript, comments, or unquoted keys).

Find documents:
{"collection":"users",
 "query":{"status":"active","createdAt":{"$gte":{"$humanTime":"30 days ago"}}},
 "fields":{"_id":1,"email":1,"plan":1},
 "sort":[{"name":"createdAt","direction":-1}],
 "limit":20}

Aggregation pipeline:
{"collection":"orders",
 "aggregate":[
  {"$match":{"status":"paid"}},
  {"$group":{"_id":"$customerId","total":{"$sum":"$amount"}}},
  {"$sort":{"total":-1}},
  {"$limit":10}]}

Count: {"collection":"users","query":{"plan":"pro"},"count":true}

Special values: ObjectId {"$oid":"64ab..."}; relative or absolute dates {"$humanTime":"3 days ago"} / {"$humanTime":"2026-01-31"}. Standard operators ($gt, $in, $regex, $exists, $elemMatch, ...) work as in MongoDB. "fields" is a projection (1=include, 0=exclude); "sort" entries are {"name":<field>,"direction":1|-1}; "skip" pages through results.

Safety: this hits a production database. Only reads are allowed ($out/$merge stages are rejected). Queries without a limit get one injected automatically. Filter and project as narrowly as possible.`

type executeQueryArgs struct {
	QueryID    int            `json:"query_id" jsonschema:"ID of the saved query to execute"`
	Parameters map[string]any `json:"parameters,omitempty" jsonschema:"Values for the query's declared parameters, keyed by name (see redash_get_query)"`
	MaxAge     *int           `json:"max_age,omitempty" jsonschema:"Freshness in seconds: 0 (default) executes fresh; -1 accepts any cached result; N accepts results younger than N seconds"`
	MaxRows    int            `json:"max_rows,omitempty" jsonschema:"Maximum rows to return (default 100, capped by server config)"`
}

type executeAdhocArgs struct {
	DataSourceID int    `json:"data_source_id" jsonschema:"ID of the MongoDB data source to query, from redash_list_data_sources"`
	Query        string `json:"query" jsonschema:"The Redash Mongo query as one JSON object string (see tool description for the format)"`
	MaxRows      int    `json:"max_rows,omitempty" jsonschema:"Maximum rows to return (default 100, capped by server config)"`
	MaxAge       *int   `json:"max_age,omitempty" jsonschema:"Freshness in seconds: 0 (default) executes fresh; -1 accepts any cached result for identical query text"`
}

type cachedResultArgs struct {
	QueryID int `json:"query_id" jsonschema:"ID of the saved query whose latest cached result to fetch"`
	MaxRows int `json:"max_rows,omitempty" jsonschema:"Maximum rows to return (default 100)"`
}

func registerExecuteTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "redash_execute_query",
		Description: "Execute a saved Redash query and wait for its rows. Queries with declared parameters need a parameters object (discover them via redash_get_query). Runs fresh by default; pass max_age -1 to reuse a cached result without touching the database.",
		Annotations: executeAnnotations("Execute saved Redash query"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args executeQueryArgs) (*mcp.CallToolResult, any, error) {
		runCtx, cancel := context.WithTimeout(ctx, d.Config.QueryTimeout)
		defer cancel()
		qr, err := d.Client.RunSavedQuery(runCtx, args.QueryID, args.Parameters, maxAge(args.MaxAge), d.Poll)
		if err != nil {
			return nil, nil, userErr(err)
		}
		text, err := format.QueryResult(qr, limitsFor(d.Config, args.MaxRows))
		if err != nil {
			return nil, nil, err
		}
		return textResult(text), nil, nil
	})

	if d.Config.DisableAdhoc {
		d.Logger.Info("redash ad-hoc query tool disabled by configuration")
	} else {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "redash_execute_adhoc_query",
			Description: "Run a new ad-hoc read query against a MongoDB data source through Redash and wait for its rows.\n\n" + mongoQueryGuide,
			Annotations: executeAnnotations("Execute ad-hoc Mongo query"),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, args executeAdhocArgs) (*mcp.CallToolResult, any, error) {
			prepared, err := mongoguard.Prepare(args.Query, d.Config.AdhocAutoLimit)
			if err != nil {
				return nil, nil, err
			}
			var notes []string
			if prepared.InjectedLimit > 0 {
				notes = append(notes, fmt.Sprintf(
					"No limit was specified, so limit %d was added automatically; pass an explicit limit (or $limit stage) to control it.",
					prepared.InjectedLimit))
			}
			runCtx, cancel := context.WithTimeout(ctx, d.Config.QueryTimeout)
			defer cancel()
			qr, err := d.Client.RunAdhoc(runCtx, args.DataSourceID, prepared.Query, maxAge(args.MaxAge), d.Poll)
			if err != nil {
				return nil, nil, userErr(err)
			}
			text, err := format.QueryResult(qr, limitsFor(d.Config, args.MaxRows), notes...)
			if err != nil {
				return nil, nil, err
			}
			return textResult(text), nil, nil
		})
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "redash_get_cached_result",
		Description: "Fetch the latest cached result of a saved query without executing anything — instant and free, but possibly stale (check retrieved_at in the response).",
		Annotations: readOnly("Get cached query result"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args cachedResultArgs) (*mcp.CallToolResult, any, error) {
		qr, err := d.Client.LatestQueryResult(ctx, args.QueryID)
		if err != nil {
			var apiErr *redash.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				return nil, nil, fmt.Errorf("query %d has no cached result yet (or is not visible to this user); run redash_execute_query to produce one", args.QueryID)
			}
			return nil, nil, userErr(err)
		}
		text, err := format.QueryResult(qr, limitsFor(d.Config, args.MaxRows))
		if err != nil {
			return nil, nil, err
		}
		return textResult(text), nil, nil
	})
}

func maxAge(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
