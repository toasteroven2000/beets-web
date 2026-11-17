package main

// Import: browsing the Import Paths, starting an Import, and answering its
// prompts until it finishes. beets-api runs the Import; the UI only shows the
// current prompt and sends Choices.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// importStartPage is the Import page: its Import Paths tab browses folders to
// import, its History tab lists the Imports beets-api has run.
type importStartPage struct {
	Tab           string // "paths" or "history"
	Status        string // History's status filter, "" for any
	AllTotal      int    // every Import, for the History tab's badge
	Browse        importBrowse
	Imports       []importOp
	Total         int // Imports beets-api has run
	Offset        int
	Active        *importOp // the running Import, if any
	ActiveID      string    // from a 409, when the list is unavailable
	Error         string
	NoImportPaths bool // BEETS_API_IMPORT_PATHS is unset or empty
	NoSources     bool // beets' config enables no metadata source
}

// Here is the directory being browsed, "" at the Import Paths themselves.
func (p importStartPage) Here() string {
	if p.Browse.Path == nil {
		return ""
	}
	return *p.Browse.Path
}

// UpURL is the browser one level up, "" at the Import Paths.
func (p importStartPage) UpURL() string {
	if p.Browse.Path == nil {
		return ""
	}
	if p.Browse.Parent == nil {
		return "/import"
	}
	return importBrowseURL(*p.Browse.Parent)
}

// importsPerPage is how many past Imports the History tab lists at once.
const importsPerPage = 50

// importStatuses are an Import's statuses, for History's filter.
var importStatuses = []string{"running", "waiting_for_choice", "completed", "aborted", "failed"}

// PathsURL is the Import Paths tab, at the folder being browsed.
// importEntriesPerPage is how many files and folders the Import Paths tab asks
// for at once.
const importEntriesPerPage = 200

// PathsPageURL is the folder being browsed, from another entry on.
func (p importStartPage) PathsPageURL(offset int) string {
	v := url.Values{}
	if p.Here() != "" {
		v.Set("path", p.Here())
	}
	if offset > 0 {
		v.Set("offset", strconv.Itoa(offset))
	}
	if len(v) == 0 {
		return "/import"
	}
	return "/import?" + v.Encode()
}

func (p importStartPage) PathsURL() string {
	if p.Here() == "" {
		return "/import"
	}
	return importBrowseURL(p.Here())
}

// HistoryURL is the History tab at an offset, keeping its filter.
func (p importStartPage) HistoryURL(offset int) string {
	v := url.Values{"tab": {"history"}}
	if p.Status != "" {
		v.Set("status", p.Status)
	}
	if offset > 0 {
		v.Set("offset", strconv.Itoa(offset))
	}
	return "/import?" + v.Encode()
}

// countsText summarises an Import in the list.
func countsText(op importOp) string {
	c := op.Counts
	if c == nil {
		return ""
	}
	if importActive(op.Status) {
		return plural(c.Choices, "Choice", "Choices") + " so far"
	}
	s := plural(c.Items, "Item", "Items") + " imported"
	if c.Items == 0 {
		s = "Nothing imported"
	}
	if c.Skipped > 0 {
		s += " · " + strconv.Itoa(c.Skipped) + " of " + plural(c.Tasks, "Import Task", "Import Tasks") + " skipped"
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func importBrowseURL(dir string) string {
	return "/import?" + url.Values{"path": {dir}}.Encode()
}

// importMode is what beets' config makes an Import do with the files.
type importMode struct {
	Text string
	Warn bool // the originals get deleted
}

// importModeFrom reads beets' import config the way beets does: move wins
// over copy, then the link kinds; with none of them files are added in place.
func importModeFrom(cfg map[string]any) importMode {
	imp, _ := cfg["import"].(map[string]any)
	on := func(k string) bool { b, _ := imp[k].(bool); return b }
	switch {
	case imp == nil:
		return importMode{}
	case on("move"):
		return importMode{"Files will be moved into the Library: the originals are deleted from the Import Path.", true}
	case on("copy") && on("delete"):
		return importMode{"Files will be copied into the Library, then the originals deleted.", true}
	case on("copy"):
		return importMode{"Files will be copied into the Library; the originals stay where they are.", false}
	case on("link"), on("hardlink"), on("reflink"):
		return importMode{"Files will be linked into the Library; the originals stay where they are.", false}
	}
	return importMode{"Files stay where they are: beets adds them to the Library in place.", false}
}

// metadataSources are the plugins shipped with beets that find Candidates.
var metadataSources = []string{"musicbrainz", "discogs", "spotify", "deezer", "beatport", "chroma"}

// noMetadataSource tells whether beets' config enables none of them, so an
// Import can't find Candidates. Setting `plugins` replaces beets' default
// [musicbrainz], which is easy to do by accident. Unknown config says false.
func noMetadataSource(cfg map[string]any) bool {
	var plugins []string
	switch v := cfg["plugins"].(type) {
	case []any:
		for _, p := range v {
			if s, ok := p.(string); ok {
				plugins = append(plugins, s)
			}
		}
	case string: // beets also takes a space-separated string
		plugins = strings.Fields(v)
	default:
		return false
	}
	for _, p := range plugins {
		for _, src := range metadataSources {
			if p == src {
				return false
			}
		}
	}
	return true
}

// CanImportAgain tells whether an Import is over, so its folders can be
// imported again.
func (op importOp) CanImportAgain() bool { return !importActive(op.Status) && !op.Undoing }

// importActive is a status in which an Import still needs the UI.
func importActive(status string) bool {
	return status == "running" || status == "waiting_for_choice"
}

func importStatusClass(status string) string {
	switch status {
	case "completed":
		return "text-bg-success"
	case "waiting_for_choice":
		return "text-bg-primary"
	case "running":
		return "text-bg-info"
	case "failed":
		return "text-bg-danger"
	}
	return "text-bg-secondary"
}

// importChoiceLabels are the buttons for Choices other than apply and search,
// with what each does.
var importChoiceLabels = map[string][2]string{
	"asis":   {"Use as-is", "Import with the tags the files have now."},
	"tracks": {"Import as tracks", "Treat each file as its own Item instead of one Album."},
	"albums": {"Group albums", "Split these files into Albums by their tags, and match each."},
	"skip":   {"Skip", "Leave these files out of the Library."},
	"keep":   {"Keep both", "Import the new files next to the ones already in the Library."},
	"remove": {"Remove old", "Import the new files and remove the existing ones from the Library, moving their files to the Trash."},
	"merge":  {"Merge", "Combine the new and existing files into one Album, then match it again."},
}

// duplicateOrder puts "Skip" first: it changes nothing.
var duplicateOrder = []string{"skip", "keep", "merge", "remove"}

// promptButtons are the plain Choices a prompt offers, in a stable order.
func promptButtons(p importPrompt) []string {
	order := []string{"asis", "tracks", "albums", "skip"}
	if p.Kind == "duplicate" {
		order = duplicateOrder
	}
	var out []string
	for _, c := range order {
		for _, have := range p.Choices {
			if c == have {
				out = append(out, c)
			}
		}
	}
	return out
}

// moreChoices are the rarer ways to answer a match prompt, kept under More.
func moreChoices(p importPrompt) []string {
	var out []string
	for _, c := range []string{"tracks", "albums"} {
		if hasChoice(p, c) {
			out = append(out, c)
		}
	}
	return out
}

func hasChoice(p importPrompt, c string) bool {
	for _, have := range p.Choices {
		if have == c {
			return true
		}
	}
	return false
}

func choiceLabel(c string) string {
	if l, ok := importChoiceLabels[c]; ok {
		return l[0]
	}
	return c
}

func choiceHelp(c string) string { return importChoiceLabels[c][1] }

// matchPercent shows a Candidate's distance as how close it is.
func matchPercent(c candidate) string {
	return strconv.Itoa(int(math.Round((1-c.Distance)*100))) + "%"
}

func matchClass(c candidate) string {
	switch {
	case c.Distance <= 0.1:
		return "text-bg-success"
	case c.Distance <= 0.3:
		return "text-bg-warning"
	}
	return "text-bg-danger"
}

func candidateName(c candidate) string {
	name := c.Album
	if name == "" {
		name = c.Title
	}
	return orDash(c.Artist) + " – " + orDash(name)
}

func candidateFiles(c candidate) []fileChanges {
	if c.Track != nil {
		return []fileChanges{*c.Track}
	}
	return c.Tracks
}

// sourceURL is a Candidate's page at its metadata source, or "".
func sourceURL(c candidate) string {
	if c.ID == "" {
		return ""
	}
	switch strings.ToLower(c.DataSource) {
	case "musicbrainz":
		if c.Track != nil {
			return "https://musicbrainz.org/recording/" + url.PathEscape(c.ID)
		}
		return "https://musicbrainz.org/release/" + url.PathEscape(c.ID)
	}
	return ""
}

// sourceIconFile is the static file of a metadata source's logo, or "".
func sourceIconFile(source string) string {
	if strings.EqualFold(source, "musicbrainz") {
		return "musicbrainz.svg"
	}
	return ""
}

// factRow is one property of a Candidate beside what the files say now.
type factRow struct {
	Label, Old, New string
	Changed         bool
}

// candidateFacts are the properties a person checks a Candidate by, shown
// whether or not they change. Album properties are the same on every file, so
// the first one speaks for all.
func candidateFacts(c candidate) []factRow {
	files := candidateFiles(c)
	if len(files) == 0 {
		return nil
	}
	f := files[0].Fields
	keys := [][2]string{{"albumartist", "Album artist"}, {"album", "Album"}, {"date", "Release date"}, {"label", "Label"},
		{"catalognum", "Catalogue no."}, {"country", "Country"}, {"media", "Media"}, {"albumtype", "Album type"}}
	if c.Track != nil {
		keys = [][2]string{{"artist", "Artist"}, {"title", "Title"}, {"album", "Album"}, {"date", "Release date"}, {"length", "Length"}}
	}
	var rows []factRow
	for _, k := range keys {
		var old, new string
		switch k[0] {
		case "date":
			old, new = dateText(f, func(c change) any { return c.Old }), dateText(f, func(c change) any { return c.New })
		case "length":
			old, new = lengthText(f["length"].Old), lengthText(f["length"].New)
		default:
			ch, ok := f[k[0]]
			if !ok {
				continue
			}
			old, new = valueString(ch.Old), valueString(ch.New)
		}
		if old == "" && new == "" {
			continue
		}
		rows = append(rows, factRow{k[1], old, new, old != new})
	}
	return rows
}

// dateText joins year, month and day into one date, leaving out zero parts.
func dateText(f map[string]change, side func(change) any) string {
	var parts []string
	for i, k := range []string{"year", "month", "day"} {
		n, _ := strconv.Atoi(valueString(side(f[k])))
		if n == 0 {
			break
		}
		if i == 0 {
			parts = append(parts, strconv.Itoa(n))
		} else {
			parts = append(parts, fmt.Sprintf("%02d", n))
		}
	}
	return strings.Join(parts, "-")
}

func lengthText(v any) string {
	f, err := strconv.ParseFloat(valueString(v), 64)
	if err != nil || f <= 0 {
		return ""
	}
	s := int(math.Round(f))
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// trackRow is one file and the track it would become.
type trackRow struct {
	File                               string
	Path                               string // the file, to play
	Disc                               string
	OldNum, OldTitle, NewNum, NewTitle string
	Length                             string
	NumChanged, TitleChanged           bool
}

// Changed tells whether Apply changes anything about the track.
func (r trackRow) Changed() bool { return r.NumChanged || r.TitleChanged }

func candidateTracks(c candidate) []trackRow {
	var rows []trackRow
	for _, f := range candidateFiles(c) {
		fields := f.Fields
		side := func(k string, old bool) string {
			ch, ok := fields[k]
			if !ok {
				return ""
			}
			if old {
				return valueString(ch.Old)
			}
			return valueString(ch.New)
		}
		r := trackRow{
			File:   baseName(f.Path),
			Path:   f.Path,
			Disc:   side("disc", false),
			OldNum: side("track", true), NewNum: side("track", false),
			OldTitle: side("title", true), NewTitle: side("title", false),
			Length: lengthText(fields["length"].Old),
		}
		if r.Length == "" {
			r.Length = lengthText(fields["length"].New)
		}
		r.NumChanged, r.TitleChanged = r.OldNum != r.NewNum, r.OldTitle != r.NewTitle
		rows = append(rows, r)
	}
	return rows
}

// discRows are the tracks of one disc, in the order beets sent them.
type discRows struct {
	Disc string
	Rows []trackRow
}

// Renamed counts the tracks Apply changes.
func (d discRows) Renamed() int {
	n := 0
	for _, r := range d.Rows {
		if r.Changed() {
			n++
		}
	}
	return n
}

// Label is the disc's heading: its number when there are several discs, then
// how many tracks it has and how many Apply changes.
func (d discRows) Label(multi bool) string {
	label := plural(len(d.Rows), "track", "tracks")
	if n := d.Renamed(); n > 0 {
		label += fmt.Sprintf(" · %d changed", n)
	} else {
		label += " · all match"
	}
	if multi {
		label = "Disc " + d.Disc + " · " + label
	}
	return label
}

// candidateDiscs groups a Candidate's tracks by disc. Tracks with no disc
// (singletons) all go in one group.
func candidateDiscs(c candidate) []discRows {
	var discs []discRows
	at := map[string]int{}
	for _, r := range candidateTracks(c) {
		i, ok := at[r.Disc]
		if !ok {
			i = len(discs)
			at[r.Disc] = i
			discs = append(discs, discRows{Disc: r.Disc})
		}
		discs[i].Rows = append(discs[i].Rows, r)
	}
	return discs
}

// changedFacts counts the album properties Apply changes.
func changedFacts(rows []factRow) int {
	n := 0
	for _, r := range rows {
		if r.Changed {
			n++
		}
	}
	return n
}

// shownFields are the fields the card shows as facts or tracks; every other
// change is bookkeeping, folded away.
var shownFields = map[string]bool{"albumartist": true, "album": true, "year": true, "month": true, "day": true, "label": true,
	"catalognum": true, "country": true, "media": true, "albumtype": true, "artist": true, "title": true, "track": true, "disc": true, "length": true}

// otherChanges are the bookkeeping fields Apply also sets, such as ids.
func otherChanges(c candidate) []string {
	seen := map[string]bool{}
	for _, f := range candidateFiles(c) {
		for k := range f.Changes {
			if !shownFields[k] {
				seen[k] = true
			}
		}
	}
	return sortedKeys(seen)
}

func otherChangesOf(f fileChanges) []string {
	var keys []string
	for _, k := range sortedKeys(f.Changes) {
		if !shownFields[k] {
			keys = append(keys, k)
		}
	}
	return keys
}

func baseName(p string) string { return path.Base(p) }

// existingScope tells whether a duplicate prompt's existing records are
// Albums: their fields are albumartist and album.
func existingScope(p importPrompt) scope {
	for _, r := range p.Existing {
		if _, ok := r["albumartist"]; ok {
			return albums
		}
	}
	return items
}

func existingName(r record) string {
	if _, ok := r["albumartist"]; ok {
		return orDash(r.Str("albumartist")) + " – " + orDash(r.Str("album"))
	}
	return orDash(r.Str("artist")) + " – " + orDash(r.Str("title"))
}

// choiceText describes a Choice already made.
func choiceText(c importChoice) string {
	switch c.Action {
	case "apply":
		if c.Candidate != nil {
			return "Applied Candidate " + strconv.Itoa(*c.Candidate+1)
		}
		return "Applied a Candidate"
	case "search":
		if len(c.SearchIDs) > 0 {
			return "Searched for " + strings.Join(c.SearchIDs, ", ")
		}
		return "Searched for " + derefOr(c.Artist) + " – " + derefOr(c.Name)
	case "abort":
		return "Aborted"
	}
	return choiceLabel(c.Action)
}

func derefOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// outcomeAlbum is the Album an imported Import Task made, or 0.
func outcomeAlbum(o importOutcome) int {
	for _, it := range o.Items {
		if id := it.Int("album_id"); id > 0 {
			return id
		}
	}
	return 0
}

func (s *server) handleImportStart(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tab := "paths"
	if q.Get("tab") == "history" {
		tab = "history"
	}
	p := s.importStartPage(r.Context(), tab, q.Get("path"), q.Get("status"), offsetParam(r))
	render(w, http.StatusOK, importStartView(p))
}

// importStartPage fills one tab of the Import page. Errors from beets-api are
// shown on the page rather than as an error page.
func (s *server) importStartPage(ctx context.Context, tab, dir, status string, offset int) importStartPage {
	p := importStartPage{Tab: tab, Status: status, Offset: offset}
	if tab == "paths" {
		b, err := s.api.ImportPaths(ctx, dir, offset)
		if err != nil {
			p.Error = sentence(errorMessage(err))
			if dir != "" {
				b, _ = s.api.ImportPaths(ctx, "", 0)
			}
		}
		p.Browse = b
		p.NoImportPaths = err == nil && b.Path == nil && len(b.Entries) == 0
		if cfg, err := s.api.Config(ctx); err == nil {
			p.NoSources = noMetadataSource(cfg)
		}
	} else {
		var statuses []string
		if status != "" {
			statuses = []string{status}
		}
		if list, err := s.api.Imports(ctx, statuses, importsPerPage, offset); err == nil {
			p.Imports, p.Total = list.Results, list.Total
		} else {
			p.Error = sentence(errorMessage(err))
		}
	}
	if status == "" && tab == "history" {
		p.AllTotal = p.Total
	} else if all, err := s.api.Imports(ctx, nil, 1, 0); err == nil {
		p.AllTotal = all.Total
	}
	// Only one Import runs at a time; it may be on a later page, or filtered out.
	if active, err := s.api.Imports(ctx, []string{"running", "waiting_for_choice"}, 1, 0); err == nil && len(active.Results) > 0 {
		p.Active = &active.Results[0]
	}
	return p
}

var activeImport = regexp.MustCompile(`import (\d+) is still active`)

func (s *server) handleCreateImport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.startImport(w, r, importRequest{
		Paths:      []string{r.PostForm.Get("path")},
		Singletons: r.PostForm.Get("singletons") != "",
	}, r.PostForm.Get("here"))
}

// handleImportAgain starts a new Import of an earlier one's folders, without
// skipping what that Import finished or skipped. Its own progress stays.
func (s *server) handleImportAgain(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	op, err := s.api.Import(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	if !op.CanImportAgain() {
		http.Redirect(w, r, "/imports/"+url.PathEscape(id), http.StatusSeeOther)
		return
	}
	resume := false
	s.startImport(w, r, importRequest{Paths: op.Request.Paths, Singletons: op.Request.Singletons, Resume: &resume}, "")
}

// startImport starts an Import and goes to it, or shows the Import page with
// why beets-api refused.
func (s *server) startImport(w http.ResponseWriter, r *http.Request, req importRequest, here string) {
	op, err := s.api.StartImport(r.Context(), req)
	if err == nil {
		http.Redirect(w, r, "/imports/"+url.PathEscape(string(op.ID)), http.StatusSeeOther)
		return
	}
	var e *apiError
	if !errors.As(err, &e) || e.Status >= 500 {
		fail(w, err)
		return
	}
	p := s.importStartPage(r.Context(), "paths", here, "", 0)
	if m := activeImport.FindStringSubmatch(e.Detail); m != nil {
		p.ActiveID = m[1]
		p.Error = fmt.Sprintf("Import %s is still active: finish or abort it first.", m[1])
	} else {
		p.Error = sentence(e.Detail)
	}
	render(w, http.StatusUnprocessableEntity, importStartView(p))
}

func (s *server) handleImport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	op, err := s.api.Import(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	var cfg map[string]any
	if op.Result.Prompt != nil && op.Result.Prompt.Kind != "duplicate" {
		cfg, _ = s.api.Config(r.Context())
	}
	q := r.URL.Query()
	pg := importPage{
		Op: op, Stale: q.Get("stale") != "", Mode: importModeFrom(cfg), Tab: importTab(q.Get("tab")), Show: importShow(q.Get("show")), Offset: offsetParam(r),
		NoSources: op.Result.Prompt != nil && len(op.Result.Prompt.Candidates) == 0 && noMetadataSource(cfg),
	}
	// What is still to come, for an Import beets-api has planned. Not having it
	// is fine: the page just doesn't list it.
	if importActive(op.Status) && op.Result.ExpectedTasks != nil {
		limit, offset := 0, 0
		switch {
		case pg.Tab == "now":
			limit = 1
		case pg.Tab == "log" && pg.Show == "all":
			limit = logPendingRows
		case pg.Tab == "log" && pg.Show == "remaining":
			limit, offset = remainingPerPage, pg.Offset
		}
		if limit > 0 {
			if list, err := s.api.ImportTasks(r.Context(), id, []string{"pending"}, limit, offset); err == nil {
				pg.Pending, pg.PendingTotal = list.Results, list.Total
			}
		}
	}
	if pr := op.Result.Prompt; pr != nil && op.Status == "waiting_for_choice" {
		s.markDuplicates(r.Context(), pr)
	}
	render(w, http.StatusOK, importView(pg))
}

func (s *server) handleImportChoice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f := r.PostForm
	req := choiceRequest{PromptID: f.Get("prompt_id"), Action: f.Get("action")}
	if n, err := strconv.Atoi(f.Get("candidate")); err == nil {
		req.Candidate = &n
	}
	if req.Action == "search" {
		if ids := strings.Fields(strings.ReplaceAll(f.Get("search_ids"), ",", " ")); len(ids) > 0 {
			req.SearchIDs = ids
		} else {
			artist, name := strings.TrimSpace(f.Get("artist")), strings.TrimSpace(f.Get("name"))
			req.Artist, req.Name = &artist, &name
		}
	}
	// Remove old moves files to the Trash, so it asks first.
	if req.Action == "remove" && f.Get("confirm") == "" {
		op, err := s.api.Import(r.Context(), id)
		if err != nil {
			fail(w, err)
			return
		}
		if op.Result.Prompt == nil || op.Result.Prompt.ID != req.PromptID {
			http.Redirect(w, r, "/imports/"+url.PathEscape(id)+"?stale=1", http.StatusSeeOther)
			return
		}
		render(w, http.StatusOK, importRemoveView(op, s.trashMissing(r)))
		return
	}
	err := s.api.ImportChoice(r.Context(), id, req)
	// 409: the prompt moved on or the Import ended; the page shows where it is.
	// But not when it is the Trash that is missing, which the page can't show.
	if isStatus(err, http.StatusConflict) && !isTrashError(err) {
		http.Redirect(w, r, "/imports/"+url.PathEscape(id)+"?stale=1", http.StatusSeeOther)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/imports/"+url.PathEscape(id), http.StatusSeeOther)
}

func (s *server) handleImportAbort(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// 409: it had already finished.
	if err := s.api.AbortImport(r.Context(), id); err != nil && !isStatus(err, http.StatusConflict) {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/imports/"+url.PathEscape(id), http.StatusSeeOther)
}

func importAuditURL(op importOp) string {
	return "/audit?" + url.Values{"operation": {string(op.ID)}}.Encode()
}

func sizeText(n *int64) string {
	if n == nil {
		return ""
	}
	const unit = 1024
	if *n < unit {
		return strconv.FormatInt(*n, 10) + " B"
	}
	f, exp := float64(*n), 0
	for f >= unit && exp < 4 {
		f /= unit
		exp++
	}
	return strconv.FormatFloat(f, 'f', 1, 64) + " " + string("KMGT"[exp-1]) + "B"
}

// shortTime trims an ISO time to the minute.
func shortTime(t string) string {
	t = strings.Replace(t, "T", " ", 1)
	if len(t) >= 16 {
		return t[:16]
	}
	return t
}

// handleImportUndo previews undoing an Import: what would go, or why it can't.
func (s *server) handleImportUndo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	op, err := s.api.Import(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	plan, err := s.api.UndoPreview(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	render(w, http.StatusOK, importUndoView(op, plan))
}

// handleImportUndoConfirm starts undoing the Import, which beets-api does in the
// background, and shows the Import while it does.
func (s *server) handleImportUndoConfirm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.api.UndoImport(r.Context(), id)
	// 409: it may not be undone, which the preview says why. Except when another
	// undo is running, which only the refusal says.
	if isStatus(err, http.StatusConflict) && !strings.Contains(errorMessage(err), "is still running") {
		http.Redirect(w, r, "/imports/"+url.PathEscape(id)+"/undo", http.StatusSeeOther)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	// It is under way, not done: the Import page follows it, then links its removals.
	http.Redirect(w, r, "/imports/"+url.PathEscape(id), http.StatusSeeOther)
}

// importedCount is how many Items an Import put in the Library.
func importedCount(op importOp) int {
	n := 0
	for _, o := range op.Result.Outcomes {
		n += len(o.Items)
	}
	return n
}

func undoHeading(plan undoPlan) string {
	if plan.Allowed {
		return "These would be removed from the Library"
	}
	return "What the Import added"
}
