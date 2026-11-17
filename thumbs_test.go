package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

type thumbTest struct {
	f     *fakeAPI
	h     http.Handler
	clock *time.Time
}

func newThumbTest(t *testing.T) thumbTest {
	t.Helper()
	f := &fakeAPI{routes: map[string]func(http.ResponseWriter, *http.Request){}}
	api := httptest.NewServer(f)
	t.Cleanup(api.Close)
	s := newServer(api.URL, api.Client())
	c, err := newThumbCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return clock }
	s.thumbs = c
	return thumbTest{f, signedIn(s.routes()), &clock}
}

// serveCover answers beets-api's GET for Album 2's art with img and etag.
func (tt thumbTest) serveCover(img []byte, etag string) {
	tt.f.routes["GET /albums/2/art"] = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", etag)
		w.Write(img)
	}
}

func (tt thumbTest) artFetches() int {
	tt.f.mu.Lock()
	defer tt.f.mu.Unlock()
	n := 0
	for _, r := range tt.f.requests {
		if r.Method == "GET" && r.URL.Path == "/albums/2/art" {
			n++
		}
	}
	return n
}

func decodeJPEG(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("not a JPEG: %v", err)
	}
	return img
}

func TestThumbIsSmallKeepsProportionsAndIsCached(t *testing.T) {
	tt := newThumbTest(t)
	tt.serveCover(pngBytes(t, 1000, 500), `"v1"`)

	rec := get(t, tt.h, "/albums/2/thumb")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if b := decodeJPEG(t, rec.Body.Bytes()).Bounds(); b.Dx() != 320 || b.Dy() != 160 {
		t.Errorf("thumbnail is %dx%d, want 320x160", b.Dx(), b.Dy())
	}
	*tt.clock = tt.clock.Add(thumbTTL - time.Minute)
	if rec := get(t, tt.h, "/albums/2/thumb"); rec.Code != 200 || rec.Body.Len() == 0 {
		t.Fatalf("cached: status %d", rec.Code)
	}
	if n := tt.artFetches(); n != 1 {
		t.Errorf("beets-api asked %d times within the period, want once", n)
	}
}

func TestThumbIsRevalidatedAfterThePeriodAndRemadeOnlyIfChanged(t *testing.T) {
	tt := newThumbTest(t)
	tt.serveCover(pngBytes(t, 400, 400), `"v1"`)
	first := get(t, tt.h, "/albums/2/thumb")

	// Same ETag, different bytes: an unchanged cover is not remade.
	tt.serveCover(pngBytes(t, 400, 200), `"v1"`)
	*tt.clock = tt.clock.Add(thumbTTL)
	same := get(t, tt.h, "/albums/2/thumb")
	if tt.artFetches() != 2 || !bytes.Equal(same.Body.Bytes(), first.Body.Bytes()) {
		t.Fatalf("after the period: %d fetches, thumbnail remade %v", tt.artFetches(), !bytes.Equal(same.Body.Bytes(), first.Body.Bytes()))
	}

	tt.serveCover(pngBytes(t, 400, 200), `"v2"`)
	*tt.clock = tt.clock.Add(thumbTTL)
	changed := get(t, tt.h, "/albums/2/thumb")
	if b := decodeJPEG(t, changed.Body.Bytes()).Bounds(); b.Dy() != 160 {
		t.Errorf("changed cover not remade: %dx%d", b.Dx(), b.Dy())
	}
	if changed.Header().Get("ETag") == first.Header().Get("ETag") {
		t.Error("the ETag stayed the same for a new cover")
	}
}

func TestThumbWithoutArtIsANotFoundThatIsCached(t *testing.T) {
	tt := newThumbTest(t)
	for range 2 {
		if rec := get(t, tt.h, "/albums/2/thumb"); rec.Code != 404 {
			t.Fatalf("status %d", rec.Code)
		}
	}
	if n := tt.artFetches(); n != 1 {
		t.Errorf("beets-api asked %d times, want once", n)
	}
}

func TestThumbOfATransparentPNGIsOnWhite(t *testing.T) {
	tt := newThumbTest(t)
	var b bytes.Buffer
	png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 10, 10)))
	tt.serveCover(b.Bytes(), `"v1"`)
	img := decodeJPEG(t, get(t, tt.h, "/albums/2/thumb").Body.Bytes())
	if r, g, bl, _ := img.At(5, 5).RGBA(); r>>8 < 250 || g>>8 < 250 || bl>>8 < 250 {
		t.Errorf("transparent pixel is %v, want white", color.RGBAModel.Convert(img.At(5, 5)))
	}
}

func TestThumbRevalidationByTheBrowserIsANotModified(t *testing.T) {
	tt := newThumbTest(t)
	tt.serveCover(pngBytes(t, 4, 4), `"v1"`)
	rec := get(t, tt.h, "/albums/2/thumb")
	etag := rec.Header().Get("ETag")
	if etag == "" || rec.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("ETag %q, Cache-Control %q", etag, rec.Header().Get("Cache-Control"))
	}
	if rec := get(t, tt.h, "/albums/2/thumb", "If-None-Match", etag); rec.Code != 304 || rec.Body.Len() != 0 {
		t.Errorf("status %d with %d bytes, want 304 and no body", rec.Code, rec.Body.Len())
	}
	if rec := get(t, tt.h, "/albums/2/thumb", "If-None-Match", `"other"`); rec.Code != 200 {
		t.Errorf("another ETag: status %d", rec.Code)
	}
}

func TestSettingOrRemovingArtRemakesTheThumb(t *testing.T) {
	tt := newThumbTest(t)
	tt.serveCover(pngBytes(t, 4, 4), `"v1"`)
	get(t, tt.h, "/albums/2/thumb")

	tt.f.routes["DELETE /albums/2/art"] = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }
	post(t, tt.h, "/albums/2/art/remove", url.Values{})
	delete(tt.f.routes, "GET /albums/2/art")
	if rec := get(t, tt.h, "/albums/2/thumb"); rec.Code != 404 {
		t.Fatalf("after Remove art: status %d", rec.Code)
	}

	tt.f.routes["GET /albums/2"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": 2, "album": "Let It Be"})
	}
	tt.f.routes["PUT /albums/2/art"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{}) }
	rec := postFile(t, tt.h, "/albums/2/art", "cover.png", pngBytes(t, 8, 8))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("Set art: status %d", rec.Code)
	}
	tt.serveCover(pngBytes(t, 8, 8), `"v2"`)
	if rec := get(t, tt.h, "/albums/2/thumb"); rec.Code != 200 {
		t.Fatalf("after Set art: status %d", rec.Code)
	}
}

func TestThumbWithoutACacheIsTheFullCover(t *testing.T) {
	f, h := newTest(t)
	img := pngBytes(t, 4, 4)
	f.routes["GET /albums/2/art"] = func(w http.ResponseWriter, r *http.Request) { w.Write(img) }
	if rec := get(t, h, "/albums/2/thumb"); rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), img) {
		t.Errorf("status %d, %d bytes", rec.Code, rec.Body.Len())
	}
}

func postFile(t *testing.T, h http.Handler, path, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(data)
	mw.Close()
	req := httptest.NewRequest("POST", path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
