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

// adhocFieldWarning is placed at the point of use — the execute tool's own
// description — because that is where the model decides what to query.
const adhocFieldWarning = `Before filtering, grouping, projecting, or sorting on any field you have not already seen in this conversation, confirm the exact field name first with redash_sample_documents (or redash_get_schema). MongoDB field names are not standardized — phone_number vs phoneNumber vs a nested contact.phone — and a wrong name returns zero rows silently instead of erroring, so a guess can look like "no data".`

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
	DataSourceID int    `json:"data_source_id,omitempty" jsonschema:"ID of the MongoDB data source to query, from redash_list_data_sources. Omit to use the configured default data source."`
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
			Description: "Run a new ad-hoc read query against a MongoDB data source through Redash and wait for its rows." + defaultDataSourceHint(d.Config.DefaultDataSource) + "\n\n" + adhocFieldWarning + "\n\n" + mongoQueryGuide,
			Annotations: executeAnnotations("Execute ad-hoc Mongo query"),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, args executeAdhocArgs) (*mcp.CallToolResult, any, error) {
			prepared, err := mongoguard.Prepare(args.Query, d.Config.AdhocAutoLimit)
			if err != nil {
				return nil, nil, err
			}
			dsID, err := resolveDataSourceID(ctx, d, args.DataSourceID)
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
			qr, err := d.Client.RunAdhoc(runCtx, dsID, prepared.Query, maxAge(args.MaxAge), d.Poll)
			if err != nil {
				return nil, nil, userErr(err)
			}
			if len(qr.Data.Rows) == 0 {
				notes = append(notes, "Query returned 0 rows. If you filtered, grouped, or projected on field names you assumed rather than verified, that is the most likely cause — confirm the real field names with redash_sample_documents (MongoDB field names vary, and a wrong name matches nothing). If an empty result is genuinely expected, ignore this.")
			}
			text, err := format.QueryResult(qr, limitsFor(d.Config, args.MaxRows), notes...)
			if err != nil {
				return nil, nil, err
			}
			return textResult(text), nil, nil
		})

		registerSampleDocumentsTool(s, d)
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
