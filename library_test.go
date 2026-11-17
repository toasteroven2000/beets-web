package main

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
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
		`class="tiles"`, `src="/albums/1/thumb"`, `loading="lazy"`, `href="/albums/1"`, "White Christmas", "Bing Crosby", "1992",
		`name="id" value="1"`, `class="selection-bar" hidden`, `src="/public/static/library.js?v=`, "21 Albums", "Artist")
	body := rec.Body.String()
	if strings.Contains(body, ">All <") || strings.Contains(body, "Modify all") || strings.Contains(body, "Remove all") {
		t.Error("with no search: acting on the whole Library")
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

func TestLibrarySearchShowsAlbumsByDefaultAndActingOnAll(t *testing.T) {
	f, h := newTest(t)
	libraryAPI(f, 2, 27)
	rec := get(t, h, "/library?q=christmas")
	mustContain(t, rec, `aria-current="page" href="/library?q=christmas">Albums`, `href="/library?q=christmas&amp;show=items">Items`, "All 2 Albums", `href="/modify?q=christmas&amp;scope=albums"`, "Remove all 2…")
	if strings.Contains(rec.Body.String(), ">All <") {
		t.Error("an All tab")
	}
	if n := len(listCalls(f)["album"]); n != 1 || listCalls(f)["album"][0].Get("limit") != "100" {
		t.Errorf("asked %v", listCalls(f))
	}
}

func TestLibraryFiltersAreChipsAndScopeActingOnAll(t *testing.T) {
	f, h := newTest(t)
	libraryAPI(f, 4, 52)
	rec := get(t, h, "/library?show=albums&year_from=1990&year_to=1999&albumtype=album")
	mustContain(t, rec, "year 1990–1999", "album type: album", `Filters`, `<span class="badge text-bg-primary">2</span>`,
		`href="/library?albumtype=album"`,                 // without the years
		`href="/library?year_from=1990&amp;year_to=1999"`, // without the type
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
	if p.ShowURL("items") != "/library?q=x&show=items" || p.URL(nil) != "/library?q=x" {
		t.Errorf("searching: %s %s", p.ShowURL("items"), p.URL(nil))
	}
	if !p.Scoped() || (libraryPage{}).Scoped() {
		t.Error("scoped")
	}
	if sortQuery("x", items, "title") != "x title+ artist+" || sortQuery("x year-", albums, "recent") != "x year-" || librarySort("nope") != "artist" {
		t.Error("sort")
	}
}

func TestLibraryArtistNamesLinkToTheLibraryFilteredToThem(t *testing.T) {
	f, h := newTest(t)
	libraryAPI(f, 2, 1)
	rec := get(t, h, "/library")
	mustContain(t, rec, `href="/library?artist=Bing+Crosby"`, `href="/library?artist=Beegie+Adair"`)
	rec = get(t, h, "/library?show=items")
	mustContain(t, rec, `href="/library?artist=Bing+Crosby"`)
}

func TestLibraryArtistFilterQueriesEachScopeByItsOwnField(t *testing.T) {
	f, h := newTest(t)
	libraryAPI(f, 2, 1)
	rec := get(t, h, "/library?artist=Bing+Crosby")
	mustContain(t, rec, "artist: Bing Crosby", "Modify all")
	calls := listCalls(f)
	if q := calls["album"][0].Get("query"); !strings.Contains(q, "albumartist::") || !strings.Contains(q, "Bing Crosby") {
		t.Errorf("Albums query: %s", q)
	}
	if q := calls["item"][0].Get("query"); !strings.Contains(q, "'artist::") {
		t.Errorf("Items query: %s", q)
	}
}

func TestNavbarHasNoSearchAndLibraryHasSearchHelp(t *testing.T) {
	f, h := newTest(t)
	libraryAPI(f, 1, 1)
	rec := get(t, h, "/library")
	body := rec.Body.String()
	if n := strings.Count(body, `role="search"`); n != 1 {
		t.Errorf("%d search forms; the Library's alone is wanted", n)
	}
	mustContain(t, rec, "Search help", `id="search-help"`, "Search uses beets' query syntax", "To use Regexp", "Common fields", `placeholder="Search the Library, or a beets query`)
	if strings.Contains(get(t, h, "/import").Body.String(), `role="search"`) {
		t.Error("a navbar search on the Import page")
	}
}

// Each fake call holds its answer until all three are in flight, so a Library
// that waits for one before asking the next never gets all three together.
func TestLibraryAsksBeetsAPIForEverythingAtOnce(t *testing.T) {
	f, h := newTest(t)
	libraryAPI(f, 21, 276)
	var mu sync.Mutex
	inFlight := 0
	all := make(chan struct{})
	together := func(next func(w http.ResponseWriter, r *http.Request)) func(w http.ResponseWriter, r *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			if inFlight++; inFlight == 3 {
				close(all)
			}
			mu.Unlock()
			select {
			case <-all:
			case <-time.After(2 * time.Second):
				t.Errorf("%s %s waited for the other calls", r.URL.Path, r.URL.Query().Get("scope"))
			}
			next(w, r)
		}
	}
	f.routes["GET /list"] = together(f.routes["GET /list"])
	f.routes["GET /config"] = together(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{}) })
	rec := get(t, h, "/library")
	mustContain(t, rec, `Albums <span class="badge text-bg-primary">21</span>`, `Items <span class="badge text-bg-secondary">276</span>`)
}

func TestLibraryArtistFilterUsesTheConfiguredSeparators(t *testing.T) {
	f, h := newTest(t)
	libraryAPI(f, 1, 1)
	f.routes["GET /config"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"beets_api": map[string]any{"artist_separators": []any{" x "}}})
	}
	get(t, h, "/library?artist=Bing")
	want := map[string]string{"album": artistQuery(albums, "Bing", []string{" x "}), "item": artistQuery(items, "Bing", []string{" x "})}
	for k, q := range want {
		if got := listCalls(f)[k][0].Get("query"); !strings.HasPrefix(got, q) {
			t.Errorf("%s query %q, want it to start %q", k, got, q)
		}
	}
}
