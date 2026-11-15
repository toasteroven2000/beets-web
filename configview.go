package main

import (
	"net/http"
	"sort"
	"strings"
)

// configRow is one setting of beets' config, with its nested path joined by
// dots (import.copy).
type configRow struct {
	Key, Value string
}

// flattenConfig lists beets' config as dotted keys in alphabetical order.
// Lists and empty sections stay on one row, so a plugin list reads as one.
func flattenConfig(cfg map[string]any) []configRow {
	var rows []configRow
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		if m, ok := v.(map[string]any); ok && len(m) > 0 {
			for k, child := range m {
				walk(prefix+"."+k, child)
			}
			return
		}
		rows = append(rows, configRow{strings.TrimPrefix(prefix, "."), valueString(v)})
	}
	walk("", cfg)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	return rows
}

// handleConfig shows beets' config as beets-api serves it, secrets redacted.
func (s *server) handleConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.api.Config(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	render(w, http.StatusOK, configView(flattenConfig(cfg)))
}
