package redashtools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mew/internal/redash"
	"mew/internal/redash/format"
)

const (
	defaultSampleCount = 3
	maxSampleCount     = 10
	// maxSampleFields and maxSampleDepth bound field-path extraction so a
	// pathological document cannot blow up the note.
	maxSampleFields = 300
	maxSampleDepth  = 6
)

type sampleDocumentsArgs struct {
	DataSourceID int            `json:"data_source_id,omitempty" jsonschema:"ID of the MongoDB data source, from redash_list_data_sources. Omit to use the configured default data source."`
	Collection   string         `json:"collection" jsonschema:"Name of the collection to sample documents from"`
	Match        map[string]any `json:"match,omitempty" jsonschema:"Optional Mongo filter to sample only matching documents (same syntax as a find query's query object); omit to sample arbitrary documents"`
	Count        int            `json:"count,omitempty" jsonschema:"How many documents to return (default 3, max 10)"`
}

// registerSampleDocumentsTool adds the document-sampling tool. It executes a
// tightly bounded find, so it is only registered when ad-hoc querying is
// enabled (it is the same capability, scoped to a handful of documents).
func registerSampleDocumentsTool(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "redash_sample_documents",
		Description: "Fetch a few real documents from a MongoDB collection so you can see the actual field names and shapes before you query. " +
			"Prefer this (or redash_get_schema) to confirm exact field names before filtering, grouping, or projecting on them: MongoDB field names are not standardized — phone_number vs phoneNumber vs a nested contact.phone — and a wrong name silently returns zero rows instead of erroring. " +
			"Returns the sampled documents plus the list of field paths observed across them." + defaultDataSourceHint(d.Config.DefaultDataSource),
		Annotations: readOnly("Sample MongoDB documents"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args sampleDocumentsArgs) (*mcp.CallToolResult, any, error) {
		collection := strings.TrimSpace(args.Collection)
		if collection == "" {
			return nil, nil, fmt.Errorf("collection is required")
		}
		count := args.Count
		if count <= 0 {
			count = defaultSampleCount
		}
		if count > maxSampleCount {
			count = maxSampleCount
		}

		dsID, err := resolveDataSourceID(ctx, d, args.DataSourceID)
		if err != nil {
			return nil, nil, err
		}

		queryObj := map[string]any{"collection": collection, "limit": count}
		if len(args.Match) > 0 {
			queryObj["query"] = args.Match
		}
		queryText, err := json.Marshal(queryObj)
		if err != nil {
			return nil, nil, fmt.Errorf("internal: building sample query: %w", err)
		}

		runCtx, cancel := context.WithTimeout(ctx, d.Config.QueryTimeout)
		defer cancel()
		qr, err := d.Client.RunAdhoc(runCtx, dsID, string(queryText), 0, d.Poll)
		if err != nil {
			return nil, nil, userErr(err)
		}

		var notes []string
		if len(qr.Data.Rows) == 0 {
			notes = append(notes, fmt.Sprintf(
				"No documents found in %q. The collection name may be wrong (list collections with redash_get_schema), the collection may be empty, or your match filter matched nothing — if you filtered on a guessed field name, that is the likely cause.",
				collection))
		} else {
			fields := collectFieldPaths(qr.Data.Columns, qr.Data.Rows)
			notes = append(notes, "Field paths observed in the sampled documents (use these exact names when querying): "+strings.Join(fields, ", "))
		}

		text, err := format.QueryResult(qr, limitsFor(d.Config, count), notes...)
		if err != nil {
			return nil, nil, err
		}
		return textResult(text), nil, nil
	})
}

// collectFieldPaths returns the sorted union of field paths across the sampled
// documents: top-level columns reported by Redash plus dotted paths for nested
// documents and []-suffixed paths for fields inside arrays of documents.
func collectFieldPaths(columns []redash.ResultColumn, rows []map[string]any) []string {
	set := make(map[string]struct{})
	for _, c := range columns {
		if c.Name != "" {
			set[c.Name] = struct{}{}
		}
	}
	for _, row := range rows {
		addFieldPaths(set, "", row, 0)
	}
	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

func addFieldPaths(set map[string]struct{}, prefix string, m map[string]any, depth int) {
	for k, v := range m {
		if len(set) >= maxSampleFields {
			return
		}
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		switch t := v.(type) {
		case map[string]any:
			// Descend unless that would exceed the depth bound, in which case
			// record the path as a leaf so the field is still reported.
			if len(t) == 0 || depth+1 > maxSampleDepth {
				set[path] = struct{}{}
			} else {
				addFieldPaths(set, path, t, depth+1)
			}
		case []any:
			set[path] = struct{}{}
			if depth+1 <= maxSampleDepth {
				for _, el := range t {
					if em, ok := el.(map[string]any); ok {
						addFieldPaths(set, path+"[]", em, depth+1)
						break
					}
				}
			}
		default:
			set[path] = struct{}{}
		}
	}
}
