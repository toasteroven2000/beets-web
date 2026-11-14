package main

// The data each page renders, and small helpers the templates share.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// tableData is a list of Albums or Items, with the query that produced it, so
// "Modify all" can Modify exactly those.
type tableData struct {
	Scope      scope
	Query      string
	Rows       []record
	Total      int
	Next       string // URL of the next batch, "" at the end
	Separators []string
}

func (t tableData) ModifyAllURL() string {
	return "/modify?" + url.Values{"scope": {string(t.Scope)}, "q": {t.Query}}.Encode()
}

type artistsPage struct {
	Query       string
	Artists     []artistSummary
	Total       int
	Unavailable bool // beets-api has no GET /artists yet
}

type artistPage struct {
	Name    string
	Tab     scope
	Queries map[scope]string
	Counts  map[scope]int
	Table   tableData
}

type listPage struct {
	Scope   scope
	Filters filters
	Table   tableData
	Error   string
}

type searchPage struct {
	Query  string
	Tables map[scope]tableData
	Error  string
}

type recordPage struct {
	Scope      scope
	Tab        string // an Album page's tab: "items", "fields" or "art"
	Record     record
	Album      record    // an Item's Album, if it has one
	Items      tableData // an Album's Items
	Separators []string

	// The Art tab.
	Art       artInfo
	ArtQuery  string // the search, prefilled with albumartist and album
	ArtPane   string // the open way to Set art: "search", "url" or "file"
	ArtURL    string // the URL last entered, kept when it fails
	ArtError  string
	ArtNotice string
}

// artInfo is an Album's current art.
type artInfo struct {
	Exists        bool
	Width, Height int
}

func (a artInfo) Dims() string { return dimsText(a.Width, a.Height) }

// artComparePage shows an Album's current art beside the image that would
// replace it.
type artComparePage struct {
	Album   record
	Token   string
	Current artInfo
	New     artInfo
	Source  string
}

// recordTabURL links to a tab of an Album page.
func recordTabURL(k scope, id int, tab string) string {
	u := recordURL(k, id)
	if tab != "" && tab != "items" {
		u += "?tab=" + tab
	}
	return u
}

type modifyPage struct {
	Scope scope
	Query string
	Count int
	Back  string
}

type modificationsPage struct {
	Mods   []modSummary
	Total  int
	Offset int
}

// popularFields are the fields most often changed, which a record's field list
// can narrow to.
var popularFields = map[scope][]string{
	items:  {"title", "artist", "album", "albumartist", "track", "tracktotal", "disc", "disctotal", "year", "genres", "composer", "grouping", "comments"},
	albums: {"album", "albumartist", "year", "original_year", "genres", "albumtype", "label", "catalognum", "country", "comp"},
}

// Fields beets manages itself, which a Modify shouldn't change.
var readOnlyFields = map[string]bool{
	"id": true, "path": true, "mtime": true, "added": true, "album_id": true, "artpath": true,
	"length": true, "bitrate": true, "bitrate_mode": true, "bitdepth": true, "channels": true,
	"samplerate": true, "format": true, "filesize": true, "encoder_info": true, "encoder_settings": true,
}

// recordFields are all of a record's fields A–Z, editable ones first.
func recordFields(r record) (editable, readOnly []string) {
	for key := range r {
		if readOnlyFields[key] {
			readOnly = append(readOnly, key)
		} else {
			editable = append(editable, key)
		}
	}
	slices.Sort(editable)
	slices.Sort(readOnly)
	return editable, readOnly
}

// knownFields suggests field names in a Modify form.
func knownFields(k scope) []string {
	fields := append([]string{}, popularFields[k]...)
	fields = append(fields, "artist_sort", "albumartist_sort", "label", "lyrics", "bpm", "mood", "style", "language", "script", "media", "albumdisambig", "comp", "month", "day", "original_month", "original_day")
	slices.Sort(fields)
	return slices.Compact(fields)
}

func (k scope) Title() string {
	if k == albums {
		return "Albums"
	}
	return "Items"
}

func (k scope) One() string {
	if k == albums {
		return "Album"
	}
	return "Item"
}

func count(n int, k scope) string {
	if n == 1 {
		return "1 " + k.One()
	}
	return strconv.Itoa(n) + " " + k.Title()
}

func artistURL(name string, tab scope) string {
	u := "/artists/" + url.PathEscape(name)
	if tab != "" {
		u += "?tab=" + string(tab)
	}
	return u
}

func recordURL(k scope, id int) string {
	return "/" + string(k) + "/" + strconv.Itoa(id)
}

func duration(r record) string {
	n, ok := r["length"]
	if !ok || n == nil {
		return ""
	}
	f, err := strconv.ParseFloat(valueString(n), 64)
	if err != nil {
		return ""
	}
	s := int(f + 0.5)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func trackNumber(r record) string {
	t := r.Int("track")
	if t == 0 {
		return ""
	}
	if d := r.Int("disc"); d > 1 {
		return fmt.Sprintf("%d-%d", d, t)
	}
	return strconv.Itoa(t)
}

func yearText(r record) string {
	if y := r.Int("year"); y != 0 {
		return strconv.Itoa(y)
	}
	return ""
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func statusClass(status string) string {
	switch status {
	case "applied":
		return "text-bg-success"
	case "awaiting_confirmation":
		return "text-bg-primary"
	case "queued", "applying":
		return "text-bg-info"
	case "partial", "rejected", "interrupted":
		return "text-bg-danger"
	}
	return "text-bg-secondary"
}

func statusText(status string) string {
	return strings.ReplaceAll(status, "_", " ")
}

func inProgress(status string) bool {
	return status == "queued" || status == "applying"
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func isPopular(k scope, field string) bool { return slices.Contains(popularFields[k], field) }

func modScope(req modRequest) scope { return req.scope() }

// previewLimit caps how many records a Preview lists, so a Modify across the
// whole Library still renders; the counts are always exact.
const previewLimit = 200

func capped(entries []previewEntry) []previewEntry {
	if len(entries) > previewLimit {
		return entries[:previewLimit]
	}
	return entries
}

func listURL(k scope, f filters, recent bool) string {
	v := url.Values{}
	set := func(k, val string) {
		if val != "" {
			v.Set(k, val)
		}
	}
	set("q", f.Query)
	set("year_from", f.YearFrom)
	set("year_to", f.YearTo)
	set("genre", f.Genre)
	set("albumtype", f.AlbumType)
	set("format", f.Format)
	if recent {
		v.Set("order", "recent")
	}
	if len(v) == 0 {
		return "/" + string(k)
	}
	return "/" + string(k) + "?" + v.Encode()
}

func pathText(v any) string { return valueString(v) }

type auditPage struct {
	Entries   []auditEntry
	Total     int
	Offset    int
	Target    string // e.g. album:5
	Action    string
	Actor     string
	Operation int // a Modification's or Import's id, 0 for any
}

// OperationName names the Modification or Import the page is filtered to.
// They share ids, so the kind comes from the entries' actions.
func (p auditPage) OperationName() string {
	for _, e := range p.Entries {
		if strings.HasSuffix(e.Action, ".modify") {
			return "Modification " + strconv.Itoa(p.Operation)
		}
		return "Import " + strconv.Itoa(p.Operation)
	}
	return "operation " + strconv.Itoa(p.Operation)
}

// operationURL links an Audit entry to the Modification or Import behind it.
// msTime shows a timestamp to the millisecond: beets-api's have microseconds.
// Anything that isn't one is shown as it is.
func msTime(s string) string {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return s
	}
	return t.Format("2006-01-02T15:04:05.000-07:00")
}

// auditEntryJSON is the whole entry as beets-api sent it, indented.
func auditEntryJSON(e auditEntry) string {
	b, _ := json.MarshalIndent(e, "", "  ")
	return string(b)
}

// operationName names what an entry's operation id refers to.
func operationName(e auditEntry) string {
	if e.OperationID == nil {
		return ""
	}
	if strings.HasSuffix(e.Action, ".modify") {
		return "Modification " + strconv.Itoa(*e.OperationID)
	}
	return "Import " + strconv.Itoa(*e.OperationID)
}

func operationURL(e auditEntry) string {
	if e.OperationID == nil {
		return ""
	}
	if strings.HasSuffix(e.Action, ".modify") {
		return "/modifications/" + strconv.Itoa(*e.OperationID)
	}
	return "/imports/" + strconv.Itoa(*e.OperationID)
}

// auditActions are the actions beets-api records, for the filter.
var auditActions = []string{"album.modify", "item.modify", "album.import", "item.import", "album.remove", "item.remove", "art.upload", "art.remove"}

func (p auditPage) URL(offset int) string {
	v := url.Values{}
	if p.Target != "" {
		v.Set("target", p.Target)
	}
	if p.Action != "" {
		v.Set("action", p.Action)
	}
	if p.Actor != "" {
		v.Set("actor", p.Actor)
	}
	if p.Operation > 0 {
		v.Set("operation", strconv.Itoa(p.Operation))
	}
	if offset > 0 {
		v.Set("offset", strconv.Itoa(offset))
	}
	if len(v) == 0 {
		return "/audit"
	}
	return "/audit?" + v.Encode()
}

// auditURL lists a record's Audit entries.
func auditURL(k scope, id int) string {
	return "/audit?" + url.Values{"target": {auditTarget(k, id)}}.Encode()
}

func auditTarget(k scope, id int) string { return k.API() + ":" + strconv.Itoa(id) }

// targetURL links an Audit target ("album:5") to its record page.
func targetURL(target string) string {
	model, id, ok := strings.Cut(target, ":")
	n, err := strconv.Atoi(id)
	if !ok || err != nil {
		return ""
	}
	switch model {
	case "album":
		return recordURL(albums, n)
	case "item":
		return recordURL(items, n)
	}
	return ""
}

// auditChange is one key of an Audit entry's changes: an old → new pair, or
// any other value as text.
type auditChange struct {
	Key      string
	Pair     bool
	Old, New string
	Text     string
}

func auditChanges(changes map[string]any) []auditChange {
	var out []auditChange
	for _, k := range sortedKeys(changes) {
		v := changes[k]
		if m, ok := v.(map[string]any); ok && len(m) == 2 {
			o, hasOld := m["old"]
			n, hasNew := m["new"]
			if hasOld && hasNew {
				out = append(out, auditChange{Key: k, Pair: true, Old: auditValue(o), New: auditValue(n)})
				continue
			}
		}
		out = append(out, auditChange{Key: k, Text: auditValue(v)})
	}
	return out
}

func auditValue(v any) string {
	switch v.(type) {
	case nil:
		return ""
	case map[string]any, []any:
		b, _ := json.MarshalIndent(v, "", "  ") // whole, however long: the Audit is where to read it
		return string(b)
	}
	return valueString(v)
}

func firstAction(entries []auditEntry) string {
	if len(entries) == 0 {
		return ""
	}
	return entries[0].Action
}
