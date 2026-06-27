// Package mongoguard validates and hardens ad-hoc Redash MongoDB (JSON)
// queries before they are allowed to reach a production database.
package mongoguard

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Result is a query that passed validation.
type Result struct {
	// Query is the (possibly rewritten) query text to submit to Redash.
	Query string
	// InjectedLimit is non-zero when a row limit was added to the query.
	InjectedLimit int
}

// Prepare validates queryText as a Redash Mongo query and injects a row
// limit when the query carries none. autoLimit <= 0 disables injection.
//
// Enforced rules:
//   - the text must be one JSON object with a string "collection" field;
//   - aggregation pipelines must not contain the write stages $out / $merge;
//   - find queries without "limit", and pipelines without a $limit or
//     $count stage, receive a limit of autoLimit.
func Prepare(queryText string, autoLimit int) (Result, error) {
	trimmed := strings.TrimSpace(queryText)
	if trimmed == "" {
		return Result{}, fmt.Errorf(`query is empty: pass a Redash Mongo query as one JSON object, e.g. {"collection":"users","query":{"status":"active"},"limit":10}`)
	}

	var q map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &q); err != nil {
		return Result{}, fmt.Errorf("query is not a valid JSON object (%v); Redash Mongo queries are strict JSON — no JavaScript, comments, or unquoted keys", err)
	}

	var collection string
	if raw, ok := q["collection"]; !ok || json.Unmarshal(raw, &collection) != nil || strings.TrimSpace(collection) == "" {
		return Result{}, fmt.Errorf(`query must set "collection" to a collection name (string)`)
	}

	if aggRaw, ok := q["aggregate"]; ok {
		return prepareAggregate(trimmed, q, aggRaw, autoLimit)
	}
	if _, ok := q["count"]; ok {
		return Result{Query: trimmed}, nil
	}
	if _, ok := q["limit"]; ok || autoLimit <= 0 {
		return Result{Query: trimmed}, nil
	}
	q["limit"] = json.RawMessage(strconv.Itoa(autoLimit))
	rewritten, err := json.Marshal(q)
	if err != nil {
		return Result{}, fmt.Errorf("internal: could not rewrite query: %w", err)
	}
	return Result{Query: string(rewritten), InjectedLimit: autoLimit}, nil
}

func prepareAggregate(original string, q map[string]json.RawMessage, aggRaw json.RawMessage, autoLimit int) (Result, error) {
	var stages []map[string]json.RawMessage
	if err := json.Unmarshal(aggRaw, &stages); err != nil {
		return Result{}, fmt.Errorf(`"aggregate" must be an array of pipeline stage objects: %v`, err)
	}

	capped := false
	for i, stage := range stages {
		for name := range stage {
			switch name {
			case "$out", "$merge":
				return Result{}, fmt.Errorf("pipeline stage %d uses %s, which writes to the database; only read queries are allowed against production MongoDB", i+1, name)
			case "$limit", "$count":
				capped = true
			}
		}
	}
	if capped || autoLimit <= 0 {
		return Result{Query: original}, nil
	}

	// A trailing $limit only caps the number of rows returned; results of
	// earlier stages ($group totals etc.) are unaffected.
	stages = append(stages, map[string]json.RawMessage{"$limit": json.RawMessage(strconv.Itoa(autoLimit))})
	newAgg, err := json.Marshal(stages)
	if err != nil {
		return Result{}, fmt.Errorf("internal: could not rewrite pipeline: %w", err)
	}
	q["aggregate"] = newAgg
	rewritten, err := json.Marshal(q)
	if err != nil {
		return Result{}, fmt.Errorf("internal: could not rewrite query: %w", err)
	}
	return Result{Query: string(rewritten), InjectedLimit: autoLimit}, nil
}
