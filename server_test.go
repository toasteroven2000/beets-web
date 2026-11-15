package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeAPI stands in for beets-api: it answers from canned handlers and
// records every request it gets.
type fakeAPI struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   []string
	routes   map[string]func(w http.ResponseWriter, r *http.Request)
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, r)
	f.bodies = append(f.bodies, string(body))
	f.mu.Unlock()
	if h, ok := f.routes[r.Method+" "+r.URL.Path]; ok {
		h(w, r)
		return
	}
	switch {
	case r.URL.Path == "/config":
		writeJSON(w, 200, map[string]any{})
	case r.URL.Path == "/list":
		writeJSON(w, 200, map[string]any{"total": 0, "limit": 100, "offset": 0, "results": []any{}})
	default:
		writeJSON(w, 404, map[string]any{"detail": "Not Found"})
	}
}

// last is the most recent request to path, and its body.
func (f *fakeAPI) last(t *testing.T, method, path string) (*http.Request, string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.requests) - 1; i >= 0; i-- {
		if f.requests[i].Method == method && f.requests[i].URL.Path == path {
			return f.requests[i], f.bodies[i]
		}
	}
	t.Fatalf("no %s %s request", method, path)
	return nil, ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// signedIn is the proxy: it sends the Actor header, which beets-ui requires, unless
// a request asks to be anonymous or sets one itself.
func signedIn(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Remote-User") == "" && r.Header.Get("X-Test-Anonymous") == "" {
			r.Header.Set("Remote-User", "alex")
		}
		h.ServeHTTP(w, r)
	})
}

func newTest(t *testing.T) (*fakeAPI, http.Handler) {
	t.Helper()
	f := &fakeAPI{routes: map[string]func(http.ResponseWriter, *http.Request){}}
	api := httptest.NewServer(f)
	t.Cleanup(api.Close)
	return f, signedIn(newServer(api.URL, api.Client()).routes())
}

func get(t *testing.T, h http.Handler, path string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func post(t *testing.T, h http.Handler, path string, form url.Values, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func mustContain(t *testing.T, rec *httptest.ResponseRecorder, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("response %d lacks %q", rec.Code, want)
		}
	}
}

func TestHomeIsOnlyASearchBar(t *testing.T) {
	_, h := newTest(t)
	rec := get(t, h, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	mustContain(t, rec, `action="/library"`, `name="q"`)
	if n := strings.Count(rec.Body.String(), `name="q"`); n != 1 {
		t.Errorf("%d search inputs, want 1", n)
	}
}

func TestHomeListsRecentlyAddedAlbums(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /list"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 1, "results": []any{
			map[string]any{"id": 7, "album": "Let It Be", "albumartist": "The Beatles"},
		}})
	}
	mustContain(t, get(t, h, "/"), "Recently added", `href="/albums/7"`, `src="/albums/7/art"`, "Let It Be", "The Beatles")
	req, _ := f.last(t, "GET", "/list")
	q := req.URL.Query()
	if q.Get("scope") != "album" || !strings.Contains(q.Get("query"), "added-") || q.Get("limit") != "12" {
		t.Errorf("list request %v", q)
	}
}

func TestHomeWithoutAlbumsOmitsRecent(t *testing.T) {
	_, h := newTest(t)
	rec := get(t, h, "/")
	if strings.Contains(rec.Body.String(), "Recently added") {
		t.Error("empty Library shows a Recently added section")
	}
}

func TestListCombinesFiltersAndLoadsMore(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /list"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 250, "limit": 100, "offset": 100, "results": []any{
			map[string]any{"id": 101, "title": "Song", "artist": "X", "album_id": 0, "length": 61.4},
		}})
	}
	rec := get(t, h, "/library?show=items&q=love&year_from=1990&sort=recent")
	mustContain(t, rec, `value="love"`, "year 1990–—", "Song", "1:01", "Singleton", `hx-trigger="revealed"`)
	var main *http.Request
	for _, r := range f.requests {
		if r.URL.Path == "/list" && r.URL.Query().Get("scope") == "item" {
			main = r
		}
	}
	if main == nil || main.URL.Query().Get("query") != "love year:1990.. added-" {
		t.Errorf("items asked with %v", main)
	}

	more := get(t, h, "/library?show=items&q=love&offset=100", "HX-Request", "true")
	if strings.Contains(more.Body.String(), "<html") {
		t.Error("more rows rendered a whole page")
	}
	mustContain(t, more, "<tr>", "offset=101")
}

func TestBadQueryShowsError(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /list"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 400, map[string]any{"detail": "invalid query: oops"})
	}
	rec := get(t, h, "/library?show=albums&q=year:x")
	if rec.Code != 400 {
		t.Errorf("status %d", rec.Code)
	}
	mustContain(t, rec, "invalid query: oops")
}

func TestRecordFormSendsOnlyChangedFields(t *testing.T) {
	f, h := newTest(t)
	f.routes["POST /modifications"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 201, map[string]any{"id": "m1", "status": "awaiting_confirmation"})
	}
	rec := post(t, h, "/modifications", url.Values{
		"scope":     {"albums"},
		"query":     {"id:2"},
		"f.album":   {"Let It Be"},
		"o.album":   {"Let It Be"},
		"f.year":    {"1970"},
		"o.year":    {"1987"},
		"f.mood":    {"happy"},
		"o.mood":    {"happy"},
		"delete":    {"mood"},
		"set_field": {"source", ""},
		"set_value": {"vinyl", "ignored"},
	})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/modifications/m1" {
		t.Fatalf("got %d %s %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	_, body := f.last(t, "POST", "/modifications")
	var got modRequest
	json.Unmarshal([]byte(body), &got)
	want := modRequest{Scope: "album", Query: "id:2", Set: map[string]string{"year": "1970", "source": "vinyl"}, Delete: []string{"mood"}}
	if got.Query != want.Query || got.Scope != want.Scope || len(got.Set) != 2 || got.Set["year"] != "1970" || got.Set["source"] != "vinyl" || len(got.Delete) != 1 || got.Delete[0] != "mood" {
		t.Errorf("sent %s", body)
	}
}

func TestRecordFormWithNoChangesIsRejected(t *testing.T) {
	_, h := newTest(t)
	rec := post(t, h, "/modifications", url.Values{"scope": {"items"}, "query": {"id:1"}, "f.title": {"A"}, "o.title": {"A"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status %d", rec.Code)
	}
	mustContain(t, rec, "nothing to change")
}

func TestModifySelectedBuildsIDsQuery(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /list"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 2, "limit": 1, "offset": 0, "results": []any{}})
	}
	rec := get(t, h, "/modify?scope=items&id=3&id=9")
	mustContain(t, rec, "Modify 2 Items", `value="id:3 , id:9"`)
	if rec := get(t, h, "/modify?scope=items"); rec.Code != 400 {
		t.Errorf("nothing selected: %d", rec.Code)
	}
}

func modificationJSON(status string, extra map[string]any) map[string]any {
	result := map[string]any{"write": true, "move": false, "preview": map[string]any{
		"albums": []any{},
		"items": []any{map[string]any{"id": 5, "label": "A - B - C", "fingerprint": "x", "changes": map[string]any{
			"year": map[string]any{"old": 1987, "new": 1970},
		}}},
	}, "applied": []any{}, "failed": []any{}}
	for k, v := range extra {
		result[k] = v
	}
	return map[string]any{"id": "m1", "status": status, "request": map[string]any{"query": "id:5", "album": false, "set": map[string]any{"year": "1970"}, "delete": []any{}}, "result": result, "created_at": "2026-11-08T10:00:00+00:00"}
}

func TestModificationPageByStatus(t *testing.T) {
	f, h := newTest(t)
	status := "awaiting_confirmation"
	extra := map[string]any{}
	f.routes["GET /modifications/m1"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, modificationJSON(status, extra))
	}

	rec := get(t, h, "/modifications/m1")
	mustContain(t, rec, "Confirm 1 Item", "<del>1987</del>", "<ins>1970</ins>", "Tags are written to files.")
	if strings.Contains(rec.Body.String(), "every 1s") {
		t.Error("polls while awaiting confirmation")
	}

	status = "applying"
	mustContain(t, get(t, h, "/modifications/m1"), `hx-trigger="every 1s"`, "Applying… 0 of 1 done.")

	status = "rejected"
	extra["stale"] = []any{map[string]any{"scope": "items", "id": 5}}
	mustContain(t, get(t, h, "/modifications/m1"), "Preview again", `href="/items/5"`)
}

func TestConfirmConflictShowsModification(t *testing.T) {
	f, h := newTest(t)
	f.routes["POST /modifications/m1/confirm"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 409, map[string]any{"detail": map[string]any{"detail": "library changed since Preview", "stale": []any{}}})
	}
	rec := post(t, h, "/modifications/m1/confirm", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/modifications/m1" {
		t.Errorf("got %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestPreviewAgainResendsRequest(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /modifications/m1"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, modificationJSON("rejected", nil))
	}
	f.routes["POST /modifications"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 201, map[string]any{"id": "m2", "status": "awaiting_confirmation"})
	}
	rec := post(t, h, "/modifications/m1/again", nil)
	if rec.Header().Get("Location") != "/modifications/m2" {
		t.Fatalf("got %d %s", rec.Code, rec.Header().Get("Location"))
	}
	_, body := f.last(t, "POST", "/modifications")
	// The stored request predates Scope, with `album: false`.
	if !strings.Contains(body, `"query":"id:5"`) || !strings.Contains(body, `"year":"1970"`) || !strings.Contains(body, `"scope":"item"`) || strings.Contains(body, `"album"`) {
		t.Errorf("sent %s", body)
	}
}

func TestAlbumPageTabs(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /albums/2"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": 2, "album": "Let It Be", "albumartist": "The Beatles", "year": 1970, "genres": []any{"Rock", "Pop"}, "added": 1.5, "mood": "happy"})
	}
	f.routes["GET /list"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 1, "limit": 1000, "offset": 0, "results": []any{
			map[string]any{"id": 7, "title": "Two of Us", "artist": "The Beatles", "album_id": 2, "track": 1, "disc": 1},
		}})
	}
	rec := get(t, h, "/albums/2")
	mustContain(t, rec, "Two of Us", `src="/albums/2/art"`, `href="/albums/2?tab=fields"`)
	if strings.Contains(rec.Body.String(), `name="f.album"`) {
		t.Error("Items tab shows the field list")
	}
	rec = get(t, h, "/albums/2?tab=fields")
	mustContain(t, rec, `name="o.genres" value="Rock; Pop"`, `name="f.mood"`, `value="id:2"`, `data-field="genres" data-popular`, "Popular only")
	if strings.Contains(rec.Body.String(), "Two of Us") {
		t.Error("Fields tab shows the Items")
	}
	if strings.Contains(rec.Body.String(), `name="f.added"`) {
		t.Error("added is editable")
	}
	body := rec.Body.String()
	if strings.Index(body, `data-field="album"`) > strings.Index(body, `data-field="mood"`) {
		t.Error("fields not A–Z")
	}
	if r, _ := f.last(t, "GET", "/list"); r.URL.Query().Get("query") != "album_id:2 disc+ track+" {
		t.Errorf("items query %s", r.URL.Query().Get("query"))
	}
}

func TestMissingRecordIs404(t *testing.T) {
	_, h := newTest(t)
	if rec := get(t, h, "/items/99"); rec.Code != 404 {
		t.Errorf("status %d", rec.Code)
	}
	if rec := get(t, h, "/albums/99/art"); rec.Code != 404 {
		t.Errorf("art status %d", rec.Code)
	}
}

func TestIconsAreServed(t *testing.T) {
	_, h := newTest(t)
	mustContain(t, get(t, h, "/"), "bootstrap-icons.min.css?v=", `class="bi bi-collection"`)
	if rec := get(t, h, "/public/static/fonts/bootstrap-icons.woff2"); rec.Code != 200 || rec.Body.Len() < 100000 {
		t.Errorf("font: %d, %d bytes", rec.Code, rec.Body.Len())
	}
}

func albumWithTracks(f *fakeAPI, album map[string]any, tracks ...map[string]any) {
	f.routes["GET /albums/2"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, album) }
	results := make([]any, len(tracks))
	for i, t := range tracks {
		results[i] = t
	}
	f.routes["GET /list"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": len(tracks), "limit": 1000, "offset": 0, "results": results})
	}
}

func track(id, disc, n int, title string) map[string]any {
	return map[string]any{"id": id, "title": title, "artist": "The Beatles", "album": "The Beatles", "album_id": 2, "disc": disc, "track": n, "length": 61.0, "year": 1968}
}

func TestAlbumTracksAreATablePerDiscWithTheTrackNumberFirst(t *testing.T) {
	f, h := newTest(t)
	// Out of order, to show the page puts them in disc and track order.
	albumWithTracks(f, map[string]any{"id": 2, "album": "The Beatles", "albumartist": "The Beatles", "year": 1968, "disctotal": 2},
		track(23, 2, 2, "Yer Blues"), track(11, 1, 2, "Dear Prudence"), track(22, 2, 1, "Birthday"), track(10, 1, 1, "Back in the U.S.S.R."))
	rec := get(t, h, "/albums/2")
	mustContain(t, rec, "1968 · 4 tracks · 2 discs · 4:04", "Disc 1", "· 2 tracks", "Disc 2")
	body := rec.Body.String()
	order := []string{"Disc 1", "Back in the U.S.S.R.", "Dear Prudence", "Disc 2", "Birthday", "Yer Blues"}
	prev := -1
	for _, s := range order {
		i := strings.Index(body, s)
		if i < prev {
			t.Fatalf("%q is out of order", s)
		}
		prev = i
	}
	if n := strings.Count(body, "<thead>"); n != 2 {
		t.Errorf("%d tables, want one per disc", n)
	}
	// The track number is the first column after the checkbox, then the title.
	if !strings.Contains(body, `<th class="select-col"><input class="form-check-input" type="checkbox" aria-label="Select all shown" data-select-all></th><th class="text-end track-col">#</th><th>Title</th>`) {
		t.Error("track number is not the first column")
	}
	// It is the number on the disc, which the table names, not "2-1".
	if strings.Contains(body, "2-1") {
		t.Error("the track number repeats the disc")
	}
	// One form: ticked rows of any disc can be Modified or removed together.
	if strings.Count(body, `type="checkbox" name="id"`) != 4 || strings.Count(body, `class="record-table"`) != 1 {
		t.Error("the discs are not one selection")
	}
}

func TestOneDiscAlbumHasNoDiscHeading(t *testing.T) {
	f, h := newTest(t)
	albumWithTracks(f, map[string]any{"id": 2, "album": "Let It Be", "albumartist": "The Beatles", "disctotal": 1},
		track(1, 1, 1, "Two of Us"), track(2, 1, 2, "Dig a Pony"))
	rec := get(t, h, "/albums/2")
	mustContain(t, rec, "2 tracks · 1 disc · 2:02", "Two of Us")
	if strings.Contains(rec.Body.String(), "Disc 1") || strings.Count(rec.Body.String(), "<thead>") != 1 {
		t.Error("a one-disc Album is split or headed")
	}
}

func TestDiscCountComesFromTheTracksWhenTheAlbumSaysNothing(t *testing.T) {
	f, h := newTest(t)
	// No disctotal (or 0), and an Item with no disc number: it is on disc 1.
	albumWithTracks(f, map[string]any{"id": 2, "album": "A", "albumartist": "B"},
		map[string]any{"id": 1, "title": "No disc", "album_id": 2, "track": 1}, track(2, 3, 1, "On three"))
	rec := get(t, h, "/albums/2")
	mustContain(t, rec, "2 tracks · 3 discs · 1:01", "Disc 1", "Disc 3", "No disc", "On three")
	if strings.Contains(rec.Body.String(), "Disc 2") {
		t.Error("shows a disc with no tracks")
	}
}

func TestDiscTitleIsShownBesideItsNumber(t *testing.T) {
	f, h := newTest(t)
	a, b := track(1, 1, 1, "x"), track(2, 2, 1, "y")
	b["disctitle"] = "Bonus"
	albumWithTracks(f, map[string]any{"id": 2, "album": "A", "albumartist": "B", "disctotal": 2}, a, b)
	mustContain(t, get(t, h, "/albums/2"), "· Bonus")
}

func TestItemsListKeepsItsColumns(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /list"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 1, "limit": 100, "offset": 0, "results": []any{track(1, 2, 3, "T")}})
	}
	rec := get(t, h, "/library?show=items")
	// Away from an Album the number stays where it was, with the disc in it.
	mustContain(t, rec, "<th>Album</th>", `<td class="text-end text-body-secondary">2-3</td>`)
	if strings.Contains(rec.Body.String(), "track-col") || strings.Contains(rec.Body.String(), "Disc 2") {
		t.Error("the Items list is laid out like an Album's tracks")
	}
}

func TestAlbumPageSaysWhichTracksTheLibraryLacks(t *testing.T) {
	f, h := newTest(t)
	// Tracks 2 and 3 of 4 are in the Library; 1 and 4 are not.
	two, three := track(2, 1, 2, "Second"), track(3, 1, 3, "Third")
	two["tracktotal"], three["tracktotal"] = 4, 4
	albumWithTracks(f, map[string]any{"id": 2, "album": "A", "albumartist": "The Beatles", "disctotal": 1}, three, two)
	rec := get(t, h, "/albums/2")
	mustContain(t, rec, "2 of 4 tracks", "Track 1 of 4 isn't in your Library", "Track 4 of 4")
	body := rec.Body.String()
	if !(strings.Index(body, "Track 1 of 4") < strings.Index(body, "Second") && strings.Index(body, "Second") < strings.Index(body, "Third") && strings.Index(body, "Third") < strings.Index(body, "Track 4 of 4")) {
		t.Error("missing tracks are not in their place")
	}
	if strings.Count(body, `type="checkbox" name="id"`) != 2 {
		t.Error("a missing track can be selected")
	}
}

func TestAlbumTracksShowFormatAndExplicitAndOnlyDifferingArtists(t *testing.T) {
	f, h := newTest(t)
	a, b := track(1, 1, 1, "Clean"), track(2, 1, 2, "Rude")
	a["format"], a["bitrate"] = "MP3", 320000
	b["format"], b["trackdisambig"] = "MP3", "explicit"
	albumWithTracks(f, map[string]any{"id": 2, "album": "A", "albumartist": "The Beatles", "disctotal": 1}, a, b)
	rec := get(t, h, "/albums/2")
	mustContain(t, rec, "MP3 · 320 kbps", `title="Explicit">E</span>`, "· MP3")
	if strings.Contains(rec.Body.String(), "<th>Artist</th>") {
		t.Error("an Artist column when every track is by the Album's artist")
	}
	// On a compilation each track names its artist.
	a["artist"] = "Someone Else"
	albumWithTracks(f, map[string]any{"id": 2, "album": "A", "albumartist": "Various Artists", "disctotal": 1}, a, b)
	mustContain(t, get(t, h, "/albums/2"), "<th>Artist</th>", "Someone Else")
}

func TestAlbumSidebarHasTheFactsAndActions(t *testing.T) {
	f, h := newTest(t)
	albumWithTracks(f, map[string]any{"id": 2, "album": "Reclassified", "albumartist": "Iggy Azalea", "year": 2014, "month": 11, "day": 24,
		"original_year": 2014, "original_month": 10, "original_day": 1, "label": "Def Jam", "country": "US", "barcode": "602547110909",
		"data_source": "MusicBrainz", "mb_albumid": "aad08360", "disctotal": 1, "added": 1794648630.0, "genres": []any{"Hip hop", "Pop"}},
		track(1, 1, 1, "Work"))
	rec := get(t, h, "/albums/2")
	mustContain(t, rec, `class="album-cover rounded border" src="/albums/2/art"`, "2014 · 1 track · 1 disc · 1:01",
		"<dt class=\"col-5 fw-normal text-body-secondary\">Released</dt>", "2014-11-24", "2014-10-01", "Def Jam", "Hip hop; Pop",
		`href="https://musicbrainz.org/release/aad08360"`, "musicbrainz.svg?v=",
		`href="/albums/2?tab=fields"`, `href="/albums/2?tab=art"`, `href="/audit?target=album%3A2"`, `action="/removals"`, `name="id" value="2"`,
		// the tabs, and the Details cards under the tracks
		`aria-current="page" href="/albums/2">Tracks`, `href="/albums/2?tab=fields">Fields`, "Details", ">Release</div>", ">Source</div>", "2026-11-14")
	// The same date is shown once.
	albumWithTracks(f, map[string]any{"id": 2, "album": "A", "albumartist": "B", "year": 1980, "month": 8, "day": 11, "original_year": 1980, "original_month": 8, "original_day": 11})
	if strings.Contains(get(t, h, "/albums/2").Body.String(), "Original release") {
		t.Error("an original release date the same as the release date")
	}
	// Other tabs keep the sidebar and drop the tracks.
	rec = get(t, h, "/albums/2?tab=fields")
	mustContain(t, rec, `class="album-cover`, `href="/albums/2">Tracks`, `aria-current="page" href="/albums/2?tab=fields">Fields`, `name="f.album"`)
}

func TestItemPageKeepsItsOwnHeader(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /items/5"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": 5, "title": "Let Down", "artist": "Radiohead", "path": "/m/x.mp3", "album_id": 0})
	}
	rec := get(t, h, "/items/5")
	mustContain(t, rec, "Let Down", "/m/x.mp3", `action="/removals"`)
	if strings.Contains(rec.Body.String(), "album-cover") {
		t.Error("an Item page has an Album's sidebar")
	}
}
