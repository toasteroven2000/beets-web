package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// libraryAPI answers /list per scope: a few Albums and Items, and how many in all.
func libraryAPI(f *fakeAPI, albumTotal, itemTotal int) {
	f.routes["GET /list"] = func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("scope") == "album" {
			writeJSON(w, 200, map[string]any{"total": albumTotal, "limit": 100, "offset": 0, "results": []any{
				map[string]any{"id": 1, "album": "White Christmas", "albumartist": "Bing Crosby", "year": 1992, "albumtype": "album"},
				map[string]any{"id": 2, "album": "Jazz Piano Christmas", "albumartist": "Beegie Adair", "year": 2008}}})
			return
		}
		writeJSON(w, 200, map[string]any{"total": itemTotal, "limit": 100, "offset": 0, "results": []any{
			map[string]any{"id": 7, "title": "White Christmas", "artist": "Bing Crosby", "album": "White Christmas", "album_id": 1, "track": 1, "length": 183.0, "year": 1992}}})
	}
}

func listCalls(f *fakeAPI) map[string][]url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string][]url.Values{}
	for _, r := range f.requests {
		if r.URL.Path == "/list" {
			out[r.URL.Query().Get("scope")] = append(out[r.URL.Query().Get("scope")], r.URL.Query())
		}
	}
	return out
}

func TestLibraryShowsAlbumsAsCoversAndCountsBoth(t *testing.T) {
	f, h := newTest(t)
	libraryAPI(f, 21, 276)
	rec := get(t, h, "/library")
	mustContain(t, rec, "<h1 class=\"h3 mb-3\">Library</h1>",
		`aria-current="page" href="/library">Albums <span class="badge text-bg-primary">21</span>`, `href="/library?show=items">Items <span class="badge text-bg-secondary">276</span>`,
		`class="tiles"`, `src="/albums/1/art"`, `loading="lazy"`, `href="/albums/1"`, "White Christmas", "Bing Crosby", "1992",
		`name="id" value="1"`, `class="selection-bar" hidden`, `src="/public/static/library.js?v=`, "21 Albums", "Artist")
	body := rec.Body.String()
	if strings.Contains(body, ">All <") || strings.Contains(body, "Modify all") || strings.Contains(body, "Remove all") {
		t.Error("with no search: an All tab, or acting on the whole Library")
	}
	// The navbar's one link is the Library, active here; its search goes here too.
	mustContain(t, rec, `class="nav-link active" aria-current="page" href="/library"`, `action="/library"`)
	// Albums are listed in pages; Items only counted.
	calls := listCalls(f)
	if calls["album"][0].Get("limit") != "100" || calls["item"][0].Get("limit") != "1" {
		t.Errorf("asked %v", calls)
	}
}

func TestLibraryAlbumsAsAList(t *testing.T) {
	f, h := newTest(t)
	libraryAPI(f, 2, 1)
	rec := get(t, h, "/library?view=list")
	mustContain(t, rec, `class="table table-sm table-hover align-middle library-table"`, `class="mini-art"`, `class="title-link" href="/albums/1"`,
		`data-select-all`, `class="btn btn-outline-secondary active" href="/library?view=list"`)
	if strings.Contains(rec.Body.String(), `class="tiles"`) {
		t.Error("covers in the list view")
	}
}

func TestLibrarySearchHasAnAllTabWithBothAndActingOnAll(t *testing.T) {
	f, h := newTest(t)
	libraryAPI(f, 2, 27)
	rec := get(t, h, "/library?q=christmas")
	mustContain(t, rec, `aria-current="page" href="/library?q=christmas&amp;show=all">All <span class="badge text-bg-primary">29</span>`,
		`href="/library?q=christmas&amp;show=albums"`, `href="/library?q=christmas&amp;show=items"`,
		"Albums <span class=\"text-body-secondary fw-normal\">2</span>", "Items <span class=\"text-body-secondary fw-normal\">27</span>",
		"See all 27 Items", "All 2 Albums", "All 27 Items", `href="/modify?q=christmas&amp;scope=albums"`, "Remove all 27…", `name="q" value="christmas"`)
	// The overview has nothing to tick: each tab is where to act on some.
	if strings.Contains(rec.Body.String(), `name="id"`) {
		t.Error("checkboxes on the All tab")
	}
	calls := listCalls(f)
	if calls["album"][0].Get("limit") != "12" || calls["item"][0].Get("limit") != "10" || !strings.HasPrefix(calls["item"][0].Get("query"), "christmas ") {
		t.Errorf("asked %v", calls)
	}
}

func TestLibraryFiltersAreChipsAndScopeActingOnAll(t *testing.T) {
	f, h := newTest(t)
	libraryAPI(f, 4, 52)
	rec := get(t, h, "/library?show=albums&year_from=1990&year_to=1999&albumtype=album")
	mustContain(t, rec, "year 1990–1999", "album type: album", `Filters`, `<span class="badge text-bg-primary">2</span>`,
		`href="/library?albumtype=album&amp;show=albums"`,                 // without the years
		`href="/library?show=albums&amp;year_from=1990&amp;year_to=1999"`, // without the type
		"Clear filters", "All 4 Albums")
	if got := listCalls(f)["album"][0].Get("query"); got != "year:1990..1999 'albumtype:album' albumartist+ year+ album+" {
		t.Errorf("query %q", got)
	}
}

func TestLibrarySortsAndKeepsItWhileSearching(t *testing.T) {
	f, h := newTest(t)
	libraryAPI(f, 2, 1)
	rec := get(t, h, "/library?sort=recent")
	mustContain(t, rec, "Recently added", `class="dropdown-item active" href="/library?sort=recent"`, `href="/library?sort=year"`, `<input type="hidden" name="sort" value="recent">`)
	if got := listCalls(f)["album"][0].Get("query"); got != "added-" {
		t.Errorf("query %q", got)
	}
}

func TestLibraryItemsTabListsAndCounts(t *testing.T) {
	f, h := newTest(t)
	libraryAPI(f, 21, 276)
	rec := get(t, h, "/library?show=items")
	mustContain(t, rec, `aria-current="page" href="/library?show=items">Items`, "276 Items", `class="title-link" href="/items/7"`,
		`class="quiet-link" href="/albums/1"`, "3:03", `<input type="hidden" name="scope" value="items">`)
	if strings.Contains(rec.Body.String(), "Show Albums as") {
		t.Error("the covers or list switch on Items")
	}
}

func TestLibraryLoadsMoreCoversAsYouScroll(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /list"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 250, "limit": 100, "offset": 100, "results": []any{map[string]any{"id": 101, "album": "More", "albumartist": "X"}}})
	}
	more := get(t, h, "/library?offset=100", "HX-Request", "true")
	if strings.Contains(more.Body.String(), "<html") {
		t.Error("the next covers rendered a whole page")
	}
	mustContain(t, more, `class="tile"`, "More", `hx-get="/library?offset=101"`)
}

func TestOldPagesRedirectToTheLibrary(t *testing.T) {
	_, h := newTest(t)
	for from, to := range map[string]string{
		"/albums":                      "/library?show=albums",
		"/items?q=love":                "/library?q=love&show=items",
		"/search?q=christmas":          "/library?q=christmas",
		"/albums?order=recent&genre=x": "/library?genre=x&show=albums&sort=recent",
	} {
		rec := get(t, h, from)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != to {
			t.Errorf("%s: %d %s, want %s", from, rec.Code, rec.Header().Get("Location"), to)
		}
	}
}

func TestRecordPagesLeadBackToTheLibrary(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /items/5"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": 5, "title": "Let Down", "artist": "Radiohead"})
	}
	mustContain(t, get(t, h, "/items/5"), `<a href="/library?show=items">Library</a>`, `class="nav-link active" aria-current="page" href="/library"`)
	albumWithTracks(f, map[string]any{"id": 2, "album": "A", "albumartist": "B"})
	mustContain(t, get(t, h, "/albums/2"), `<a href="/library">Library</a>`)
}

func TestLibraryURL(t *testing.T) {
	p := libraryPage{Show: "albums", View: "grid", Sort: "artist"}
	if p.URL(nil) != "/library" || p.ShowURL("items") != "/library?show=items" || p.ViewURL("list") != "/library?view=list" || p.SortURL("year") != "/library?sort=year" {
		t.Error("plain URLs")
	}
	p.Filters.Query = "x"
	if p.ShowURL("all") != "/library?q=x&show=all" || p.URL(nil) != "/library?q=x&show=albums" {
		t.Errorf("searching: %s %s", p.ShowURL("all"), p.URL(nil))
	}
	if !p.Scoped() || (libraryPage{}).Scoped() {
		t.Error("scoped")
	}
	if sortQuery("x", items, "title") != "x title+ artist+" || sortQuery("x year-", albums, "recent") != "x year-" || librarySort("nope") != "artist" {
		t.Error("sort")
	}
}
