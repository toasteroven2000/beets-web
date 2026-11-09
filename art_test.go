package main

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// newArtTest is newTest with the art sources pointed at a fake web.
func newArtTest(t *testing.T, web http.HandlerFunc) (*fakeAPI, http.Handler, string) {
	t.Helper()
	f := &fakeAPI{routes: map[string]func(http.ResponseWriter, *http.Request){}}
	api := httptest.NewServer(f)
	t.Cleanup(api.Close)
	ws := httptest.NewServer(web)
	t.Cleanup(ws.Close)
	s := newServer(api.URL, api.Client())
	s.art.itunes = ws.URL + "/itunes"
	s.art.deezer = ws.URL + "/deezer"
	return f, s.routes(), ws.URL
}

func album(artpath any) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": 2, "album": "Let It Be", "albumartist": "The Beatles", "artpath": artpath})
	}
}

func noItems(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"total": 0, "limit": 1000, "offset": 0, "results": []any{}})
}

func TestArtTabPrefillsSearchAndLinksFullSize(t *testing.T) {
	f, h, _ := newArtTest(t, http.NotFound)
	f.routes["GET /albums/2"] = album("/music/cover.png")
	f.routes["GET /list"] = noItems
	img := pngBytes(t, 40, 30)
	f.routes["GET /albums/2/art"] = func(w http.ResponseWriter, r *http.Request) { w.Write(img) }
	rec := get(t, h, "/albums/2?tab=art")
	mustContain(t, rec, `value="The Beatles Let It Be"`, `hx-get="/albums/2/art/search"`, "40×30", `href="/albums/2/art" target="_blank"`, "Full size", `class="tab-pane show active" id="art-search"`)
}

func TestArtSearchWithoutAlbumRanksBySize(t *testing.T) {
	var base string
	small, big := pngBytes(t, 10, 10), pngBytes(t, 30, 30)
	_, h, ws := newArtTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/itunes":
			if r.URL.Query().Get("term") != "beatles let it be" {
				t.Errorf("term %q", r.URL.Query().Get("term"))
			}
			writeJSON(w, 200, map[string]any{"results": []any{
				map[string]any{"artistName": "The Beatles", "collectionName": "Small", "artworkUrl100": base + "/small/100x100bb.jpg"},
				map[string]any{"artistName": "The Beatles", "collectionName": "Gone", "artworkUrl100": base + "/gone/100x100bb.jpg"},
			}})
		case "/deezer":
			writeJSON(w, 200, map[string]any{"data": []any{
				map[string]any{"title": "Big", "cover_xl": base + "/big.png", "link": "https://deezer.example/1", "artist": map[string]any{"name": "The Beatles"}},
			}})
		case "/small/3000x3000bb.jpg":
			w.Write(small)
		case "/big.png":
			w.Write(big)
		default:
			http.NotFound(w, r)
		}
	})
	base = ws
	body := get(t, h, "/albums/2/art/search?q=beatles+let+it+be").Body.String()
	bigAt, smallAt := strings.Index(body, "The Beatles — Big"), strings.Index(body, "The Beatles — Small")
	if bigAt < 0 || smallAt < 0 || bigAt > smallAt {
		t.Errorf("want Big before Small:\n%s", body)
	}
	if strings.Contains(body, "Gone") {
		t.Error("kept a candidate whose image is missing")
	}
	for _, want := range []string{"30×30", "10×10", `href="https://deezer.example/1"`, `value="` + base + `/big.png"`} {
		if !strings.Contains(body, want) {
			t.Errorf("lacks %q", want)
		}
	}
}

func TestArtSearchReportsAFailingSource(t *testing.T) {
	_, h, _ := newArtTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/deezer" {
			http.Error(w, "down", 500)
			return
		}
		writeJSON(w, 200, map[string]any{"results": []any{}})
	})
	mustContain(t, get(t, h, "/albums/2/art/search?q=x"), "Deezer: 500 Internal Server Error", "iTunes: no results")
}

func TestSetArtWithoutArtSetsItStraightAway(t *testing.T) {
	img := pngBytes(t, 5, 5)
	f, h, ws := newArtTest(t, func(w http.ResponseWriter, r *http.Request) { w.Write(img) })
	f.routes["GET /albums/2"] = album(nil)
	f.routes["PUT /albums/2/art"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{}) }
	rec := post(t, h, "/albums/2/art", url.Values{"url": {ws + "/a.png"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/albums/2?tab=art&set=1" {
		t.Fatalf("status %d location %q", rec.Code, rec.Header().Get("Location"))
	}
	r, body := f.last(t, "PUT", "/albums/2/art")
	if body != string(img) || r.Header.Get("Content-Type") != "image/png" {
		t.Errorf("PUT %q %d bytes", r.Header.Get("Content-Type"), len(body))
	}
}

func TestSetArtOverExistingArtComparesFirst(t *testing.T) {
	img := pngBytes(t, 8, 6)
	f, h, ws := newArtTest(t, func(w http.ResponseWriter, r *http.Request) { w.Write(img) })
	f.routes["GET /albums/2"] = album("/music/cover.jpg")
	f.routes["GET /albums/2/art"] = func(w http.ResponseWriter, r *http.Request) { w.Write(pngBytes(t, 4, 4)) }
	f.routes["PUT /albums/2/art"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{}) }

	rec := post(t, h, "/albums/2/art", url.Values{"url": {ws + "/a.png"}})
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(loc, "/albums/2/art/new/") {
		t.Fatalf("status %d location %q", rec.Code, loc)
	}
	for _, r := range f.requests {
		if r.Method == "PUT" {
			t.Fatal("set art before the comparison")
		}
	}
	mustContain(t, get(t, h, loc), "4×4", "8×6", ws+"/a.png", `action="`+loc+`"`)
	if got := get(t, h, loc+"/image").Body.String(); got != string(img) {
		t.Error("pending image differs")
	}
	if rec := post(t, h, loc, nil); rec.Header().Get("Location") != "/albums/2?tab=art&set=1" {
		t.Fatalf("confirm went to %q", rec.Header().Get("Location"))
	}
	if _, body := f.last(t, "PUT", "/albums/2/art"); body != string(img) {
		t.Error("PUT a different image")
	}
	if rec := get(t, h, loc); rec.Code != 404 {
		t.Errorf("token still usable: %d", rec.Code)
	}
}

func TestSetArtRejectsNonImages(t *testing.T) {
	f, h, ws := newArtTest(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>nope</html>")) })
	f.routes["GET /albums/2"] = album(nil)
	f.routes["GET /list"] = noItems
	rec := post(t, h, "/albums/2/art", url.Values{"url": {ws + "/page"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d", rec.Code)
	}
	mustContain(t, rec, "must be a JPEG or PNG", `value="`+ws+`/page"`, `class="tab-pane show active" id="art-url"`)
}

func TestNormName(t *testing.T) {
	for in, want := range map[string]string{
		"Let It Be (Remastered 2009)": "letitbe",
		"Let It Be [Super Deluxe]":    "letitbe",
		"Simon & Garfunkel":           "simonandgarfunkel",
		"Simon and Garfunkel":         "simonandgarfunkel",
		"Sgt. Pepper's Lonely Hearts": "sgtpepperslonelyhearts",
	} {
		if got := normName(in); got != want {
			t.Errorf("normName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRankArtPrefersTheMatchingRelease(t *testing.T) {
	found := []artCandidate{
		{Label: "tribute", Artist: "Various Artists", Title: "Let It Be: A Tribute", Width: 3000, Height: 3000, rank: 0},
		{Label: "naked", Artist: "The Beatles", Title: "Let It Be... Naked", Tracks: 11, Year: 2003, Width: 3000, Height: 3000, rank: 1},
		{Label: "deezer", Artist: "The Beatles", Title: "Let It Be (Remastered)", Tracks: 12, Width: 1000, Height: 1000, rank: 0},
		{Label: "itunes", Artist: "The Beatles", Title: "Let It Be (Remastered)", Tracks: 12, Year: 1970, Width: 1400, Height: 1400, rank: 2},
		{Label: "deluxe", Artist: "The Beatles", Title: "Let It Be (Super Deluxe)", Tracks: 57, Year: 1970, Width: 3000, Height: 3000, rank: 3},
	}
	rankArt(found, artTarget{Artist: "The Beatles", Title: "Let It Be", Tracks: 12, Year: 1970})
	var got []string
	for _, c := range found {
		got = append(got, c.Label)
	}
	if want := "itunes deezer deluxe naked tribute"; strings.Join(got, " ") != want {
		t.Errorf("order %v, want %s", got, want)
	}
	if !found[0].TracksMatch || !found[0].YearMatch || found[2].TracksMatch {
		t.Error("match flags wrong")
	}
}

func TestArtSearchRanksAgainstTheAlbum(t *testing.T) {
	var base string
	img := pngBytes(t, 10, 10)
	f, h, ws := newArtTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/itunes":
			writeJSON(w, 200, map[string]any{"results": []any{
				map[string]any{"artistName": "Someone Else", "collectionName": "Let It Be", "artworkUrl100": base + "/a/100x100bb.jpg", "trackCount": 3},
				map[string]any{"artistName": "The Beatles", "collectionName": "Let It Be (Remastered)", "artworkUrl100": base + "/b/100x100bb.jpg", "trackCount": 12, "releaseDate": "1970-05-08T07:00:00Z"},
			}})
		case "/deezer":
			writeJSON(w, 200, map[string]any{"data": []any{}})
		default:
			w.Write(img)
		}
	})
	base = ws
	f.routes["GET /albums/2"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": 2, "album": "Let It Be", "albumartist": "The Beatles", "year": 1970})
	}
	f.routes["GET /list"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 12, "limit": 1, "offset": 0, "results": []any{}})
	}
	body := get(t, h, "/albums/2/art/search?q=x").Body.String()
	if strings.Index(body, "The Beatles — Let It Be (Remastered)") > strings.Index(body, "Someone Else — Let It Be") {
		t.Errorf("Someone Else ranked first:\n%s", body)
	}
	for _, want := range []string{`<span class="text-success">12 tracks</span>`, `<span class="text-success">1970</span>`, "3 tracks"} {
		if !strings.Contains(body, want) {
			t.Errorf("lacks %q", want)
		}
	}
}
