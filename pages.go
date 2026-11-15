package main

// The data each page renders, and small helpers the templates share.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
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
	Discs      []discTable // an Album's Items, a table per disc, instead of Rows
	ShowArtist bool        // the Album's tracks are by different artists
}

// discTable is the tracks of one disc of an Album.
type discTable struct {
	Number int
	Title  string // the disc's own title, if it has one
	Rows   []record
}

// discTables groups an Album's Items by disc, in disc order and then track
// order. An Item with no disc number is on disc 1.
func discTables(rows []record) []discTable {
	var discs []discTable
	at := map[int]int{}
	for _, r := range rows {
		n := max(r.Int("disc"), 1)
		i, ok := at[n]
		if !ok {
			i = len(discs)
			at[n] = i
			discs = append(discs, discTable{Number: n})
		}
		discs[i].Rows = append(discs[i].Rows, r)
		if discs[i].Title == "" {
			discs[i].Title = r.Str("disctitle")
		}
	}
	slices.SortFunc(discs, func(a, b discTable) int { return a.Number - b.Number })
	for _, d := range discs {
		slices.SortStableFunc(d.Rows, func(a, b record) int { return a.Int("track") - b.Int("track") })
	}
	return discs
}

// albumDiscCount is how many discs an Album has: what it says, or the highest
// disc its Items are on if it says nothing.
func albumDiscCount(album record, discs []discTable) int {
	n := max(album.Int("disctotal"), 1)
	for _, d := range discs {
		n = max(n, d.Number)
	}
	return n
}

func (t tableData) ModifyAllURL() string {
	return "/modify?" + url.Values{"scope": {string(t.Scope)}, "q": {t.Query}}.Encode()
}

type recordPage struct {
	Scope      scope
	Tab        string // an Album page's tab: "items", "fields" or "art"
	Record     record
	Album      record    // an Item's Album, if it has one
	Items      tableData // an Album's Items
	Info       albumInfo // an Album's summary, facts and details
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

type removalsPage struct {
	Removals []removalSummary
	Total    int
	Offset   int
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

// audioExtensions are the files beets-api will serve for playing.
var audioExtensions = map[string]bool{".mp3": true, ".flac": true, ".ogg": true, ".oga": true, ".opus": true, ".m4a": true, ".mp4": true, ".aac": true, ".wav": true, ".aif": true, ".aiff": true}

func isAudioFile(name string) bool { return audioExtensions[strings.ToLower(path.Ext(name))] }

// importAudioURL plays a file waiting under an Import Path.
func importAudioURL(p string) string {
	return "/import-paths/audio?" + url.Values{"path": {p}}.Encode()
}

// artistLibraryURL is the Library filtered to one Artist.
func artistLibraryURL(name string) string {
	return "/library?" + url.Values{"artist": {name}}.Encode()
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

// missingSpan is how many columns a missing track's message spans.
func missingSpan(t tableData) string {
	if t.ShowArtist {
		return "4"
	}
	return "3"
}

// trackOnDisc is an Item's track number on its own disc, which the table it is
// in already names.
func trackOnDisc(r record) string {
	if t := r.Int("track"); t != 0 {
		return strconv.Itoa(t)
	}
	return ""
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
	case "previewing", "queued", "applying":
		return "text-bg-info"
	case "partial", "rejected", "interrupted", "failed":
		return "text-bg-danger"
	}
	return "text-bg-secondary"
}

func statusText(status string) string {
	return strings.ReplaceAll(status, "_", " ")
}

func inProgress(status string) bool {
	return status == "previewing" || status == "queued" || status == "applying"
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

// OperationName names the Modification, Removal or Import the page is
// filtered to. They share ids, so the kind comes from the entries.
func (p auditPage) OperationName() string {
	if len(p.Entries) == 0 {
		return "operation " + strconv.Itoa(p.Operation)
	}
	return operationKind(p.Entries[0]) + " " + strconv.Itoa(p.Operation)
}

// OperationLink is where the Modification, Removal or Import the page is
// filtered to can be read.
func (p auditPage) OperationLink() string {
	if len(p.Entries) == 0 {
		return ""
	}
	e := p.Entries[0]
	e.OperationID = &p.Operation
	return operationURL(e)
}

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
// operationKind says what an entry's operation id refers to. Imports remove
// records too, to replace duplicates or to undo themselves; those entries say
// so, and the rest of the removals are Removals.
func operationKind(e auditEntry) string {
	_, replaced := e.Changes["replaced_by"]
	_, undone := e.Changes["undo_of_import"]
	switch {
	case strings.HasSuffix(e.Action, ".modify"):
		return "Modification"
	case e.Action == "trash.purge":
		return "Purge"
	case strings.HasSuffix(e.Action, ".remove") && !replaced && !undone:
		return "Removal"
	}
	return "Import"
}

func operationName(e auditEntry) string {
	if e.OperationID == nil {
		return ""
	}
	return operationKind(e) + " " + strconv.Itoa(*e.OperationID)
}

// operationURL links an Audit entry to the Modification, Removal or Import behind it.
func operationURL(e auditEntry) string {
	if e.OperationID == nil {
		return ""
	}
	switch operationKind(e) {
	case "Modification":
		return "/modifications/" + strconv.Itoa(*e.OperationID)
	case "Removal":
		return "/removals/" + strconv.Itoa(*e.OperationID)
	case "Purge":
		return "/purges/" + strconv.Itoa(*e.OperationID)
	}
	return "/imports/" + strconv.Itoa(*e.OperationID)
}

// auditActions are the actions beets-api records, for the filter.
var auditActions = []string{"album.modify", "item.modify", "album.import", "item.import", "album.remove", "item.remove", "art.upload", "art.remove", "trash.purge"}

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
	if ok && model == "trash" { // a purged Trash folder: the Trash is where to look
		return "/trash"
	}
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
