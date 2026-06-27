package redashtools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"mew/internal/redash"
)

// resolveDataSourceID turns an optional data_source_id argument into a concrete
// id. When argID is zero it falls back to the configured default data source —
// either a numeric id or a name resolved against the visible sources — and
// returns a guiding error when neither is available.
func resolveDataSourceID(ctx context.Context, d Deps, argID int) (int, error) {
	if argID > 0 {
		return argID, nil
	}
	def := strings.TrimSpace(d.Config.DefaultDataSource)
	if def == "" {
		return 0, fmt.Errorf("data_source_id is required and no default is configured; list options with redash_list_data_sources, or set a default with `mew config set redash.default_data_source`")
	}
	if id, err := strconv.Atoi(def); err == nil {
		return id, nil
	}
	sources, err := d.Client.DataSources(ctx)
	if err != nil {
		return 0, userErr(err)
	}
	if id, ok := findDataSourceByName(sources, def); ok {
		return id, nil
	}
	names := make([]string, 0, len(sources))
	for _, ds := range sources {
		names = append(names, ds.Name)
	}
	return 0, fmt.Errorf("configured default data source %q was not found among the visible sources: %s", def, strings.Join(names, ", "))
}

// findDataSourceByName returns the id of the source whose name matches ref
// case-insensitively.
func findDataSourceByName(sources []redash.DataSource, ref string) (int, bool) {
	ref = strings.TrimSpace(ref)
	for _, ds := range sources {
		if strings.EqualFold(strings.TrimSpace(ds.Name), ref) {
			return ds.ID, true
		}
	}
	return 0, false
}

// isDefaultSource reports whether ds is the configured default (matched by id
// or, when def is non-numeric, by case-insensitive name).
func isDefaultSource(ds redash.DataSource, def string) bool {
	def = strings.TrimSpace(def)
	if def == "" {
		return false
	}
	if id, err := strconv.Atoi(def); err == nil {
		return ds.ID == id
	}
	return strings.EqualFold(strings.TrimSpace(ds.Name), def)
}

// defaultDataSourceHint augments a tool description when a default data source
// is configured, so the model treats omitting data_source_id as the norm.
func defaultDataSourceHint(defaultDataSource string) string {
	def := strings.TrimSpace(defaultDataSource)
	if def == "" {
		return ""
	}
	return fmt.Sprintf(" Omit data_source_id to use the default data source (%q); pass an explicit id only when the user asks for a different source.", def)
}
