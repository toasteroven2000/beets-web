package main

// Building beets query strings. beets splits a query string with Python's
// shlex.split (POSIX rules), so every term the UI builds is single-quoted.

import (
	"regexp"
	"strconv"
	"strings"
)

// scope is what a list or Modification is about: Albums or Items, never both.
type scope string

const (
	albums scope = "albums"
	items  scope = "items"
)

// API is the scope as beets-api names it.
func (k scope) API() string {
	if k == albums {
		return "album"
	}
	return "item"
}

func parseScope(s string) (scope, bool) {
	switch scope(s) {
	case albums, items:
		return scope(s), true
	}
	return "", false
}

var defaultSeparators = []string{",", ";", "&"}

// shellQuote quotes s as one shlex token.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// term is one `field:value` query term, quoted.
func term(field, value string) string {
	return shellQuote(field + ":" + value)
}

// artistQuery matches every Album (on albumartist) or Item (on artist) whose
// credit names the Artist, as a whole name between Artist separators. See
// docs/adr/0001. The pattern is valid in both Go and Python `re`.
func artistQuery(k scope, name string, separators []string) string {
	field := "artist"
	if k == albums {
		field = "albumartist"
	}
	return shellQuote(field + "::" + artistPattern(name, separators))
}

func artistPattern(name string, separators []string) string {
	sep := make([]string, len(separators))
	for i, s := range separators {
		sep[i] = regexp.QuoteMeta(s)
	}
	n := regexp.QuoteMeta(strings.TrimSpace(name))
	if len(sep) == 0 {
		return `^\s*` + n + `\s*$`
	}
	alt := "(?:" + strings.Join(sep, "|") + ")"
	return `^(?:.*` + alt + `)?\s*` + n + `\s*(?:` + alt + `.*)?$`
}

// splitArtists divides one credit into the Artists it names.
func splitArtists(credit string, separators []string) []string {
	parts := []string{credit}
	for _, sep := range separators {
		if sep == "" {
			continue
		}
		var next []string
		for _, p := range parts {
			next = append(next, strings.Split(p, sep)...)
		}
		parts = next
	}
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// idsQuery matches exactly the given records, joined with beets' `,` OR.
func idsQuery(ids []int) string {
	terms := make([]string, len(ids))
	for i, id := range ids {
		terms[i] = "id:" + strconv.Itoa(id)
	}
	return strings.Join(terms, " , ")
}

// filters are the structured filters on the Albums and Items lists. With the
// raw query they make one beets query, which the page shows and lets you edit.
type filters struct {
	Query     string
	YearFrom  string
	YearTo    string
	Genre     string
	AlbumType string
	Format    string
	Artist    string // a whole Artist name, matched on albumartist for Albums and artist for Items
}

// Combined is the query for k: the raw query and every filter.
func (f filters) Combined(k scope, separators []string) string {
	var terms []string
	if q := strings.TrimSpace(f.Query); q != "" {
		terms = append(terms, q)
	}
	if f.Artist != "" {
		terms = append(terms, artistQuery(k, f.Artist, separators))
	}
	if f.YearFrom != "" || f.YearTo != "" {
		terms = append(terms, "year:"+f.YearFrom+".."+f.YearTo)
	}
	if f.Genre != "" {
		terms = append(terms, term("genres", f.Genre))
	}
	if f.AlbumType != "" {
		terms = append(terms, term("albumtype", f.AlbumType))
	}
	if f.Format != "" {
		terms = append(terms, term("format", f.Format))
	}
	return strings.Join(terms, " ")
}

func (f filters) Active() bool {
	return f.Artist != "" || f.YearFrom != "" || f.YearTo != "" || f.Genre != "" || f.AlbumType != "" || f.Format != ""
}

// hasSort reports whether a query has a beets sort term: one ending in + or -
// with no field. Quoted terms never look like that, so a plain split is enough.
func hasSort(query string) bool {
	for _, part := range strings.Fields(query) {
		if len(part) > 1 && (strings.HasSuffix(part, "+") || strings.HasSuffix(part, "-")) && !strings.Contains(part, ":") {
			return true
		}
	}
	return false
}
