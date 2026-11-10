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

func newTest(t *testing.T) (*fakeAPI, http.Handler) {
	t.Helper()
	f := &fakeAPI{routes: map[string]func(http.ResponseWriter, *http.Request){}}
	api := httptest.NewServer(f)
	t.Cleanup(api.Close)
	return f, newServer(api.URL, api.Client()).routes()
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

func post(t *testing.T, h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
	mustContain(t, rec, `action="/search"`, `name="q"`)
	if n := strings.Count(rec.Body.String(), `name="q"`); n != 1 {
		t.Errorf("%d search inputs, want 1", n)
	}
}

func TestArtistsWithoutEndpointSaysSo(t *testing.T) {
	_, h := newTest(t)
	rec := get(t, h, "/artists")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	mustContain(t, rec, "has no <code>GET /artists</code>")
}

func TestArtistsList(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /artists"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 2, "results": []any{
			map[string]any{"name": "AC/DC", "album_count": 2, "item_count": 20},
			map[string]any{"name": "Beyoncé", "album_count": 0, "item_count": 1},
		}})
	}
	rec := get(t, h, "/artists?q=a")
	mustContain(t, rec, "2 matching", `href="/artists/AC%2FDC"`, "2 Albums · 20 Items", "1 Item<")
	if r, _ := f.last(t, "GET", "/artists"); r.URL.Query().Get("query") != "a" {
		t.Errorf("query not passed: %s", r.URL.RawQuery)
	}
}

func TestArtistPageQueriesByWholeName(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /config"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"beets_api": map[string]any{"artist_separators": []string{"&"}}})
	}
	f.routes["GET /list"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 1, "limit": 100, "offset": 0, "results": []any{
			map[string]any{"id": 4, "album": "Back in Black", "albumartist": "AC/DC & Friends", "year": 1980},
		}})
	}
	rec := get(t, h, "/artists/AC%2FDC")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	mustContain(t, rec, "Back in Black", `href="/artists/Friends"`, "Modify all 1 Album")
	r, _ := f.last(t, "GET", "/list")
	if got := r.URL.Query().Get("scope"); got != "album" {
		t.Errorf("scope %q", got)
	}
	want := `'albumartist::^(?:.*(?:&))?\s*AC/DC\s*(?:(?:&).*)?$' albumartist+ year+ album+`
	if got := r.URL.Query().Get("query"); got != want {
		t.Errorf("query %s", got)
	}
}

func TestListCombinesFiltersAndLoadsMore(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /list"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 250, "limit": 100, "offset": 100, "results": []any{
			map[string]any{"id": 101, "title": "Song", "artist": "X", "album_id": 0, "length": 61.4},
		}})
	}
	rec := get(t, h, "/items?q=love&year_from=1990&order=recent")
	mustContain(t, rec, "<code class=\"small\">love year:1990..</code>", "Song", "1:01", "Singleton", `hx-trigger="revealed"`)
	r, _ := f.last(t, "GET", "/list")
	if got := r.URL.Query().Get("query"); got != "love year:1990.. added-" {
		t.Errorf("query %s", got)
	}

	more := get(t, h, "/items?q=love&offset=100", "HX-Request", "true")
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
	rec := get(t, h, "/albums?q=year:x")
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

func TestModificationsWithoutEndpointSaysSo(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /modifications"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 405, map[string]any{"detail": "Method Not Allowed"})
	}
	mustContain(t, get(t, h, "/modifications"), "has no <code>GET /modifications</code>")
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
	mustContain(t, get(t, h, "/"), "bootstrap-icons.min.css?v=", `class="bi bi-disc"`)
	if rec := get(t, h, "/public/static/fonts/bootstrap-icons.woff2"); rec.Code != 200 || rec.Body.Len() < 100000 {
		t.Errorf("font: %d, %d bytes", rec.Code, rec.Body.Len())
	}
}
