package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestAudioIsRelayedWithItsRange(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /items/7/audio"] = func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=0-9" {
			t.Errorf("Range %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Range", "bytes 0-9/100")
		w.WriteHeader(http.StatusPartialContent)
		w.Write([]byte("0123456789"))
	}
	rec := get(t, h, "/items/7/audio", "Range", "bytes=0-9")
	if rec.Code != http.StatusPartialContent || rec.Header().Get("Content-Range") != "bytes 0-9/100" || rec.Header().Get("Content-Type") != "audio/mpeg" || rec.Body.String() != "0123456789" {
		t.Errorf("%d %v %q", rec.Code, rec.Header(), rec.Body)
	}
}

func TestAudioRefusalsAreRelayed(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /import-paths/audio"] = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("path") != "/etc/passwd" {
			t.Errorf("path %q", r.URL.Query().Get("path"))
		}
		writeJSON(w, 403, map[string]any{"detail": "path is outside BEETS_API_IMPORT_PATHS"})
	}
	if rec := get(t, h, "/import-paths/audio?path=/etc/passwd"); rec.Code != 403 {
		t.Errorf("status %d", rec.Code)
	}
}

func TestLibraryItemsHavePlayButtons(t *testing.T) {
	f, h := newTest(t)
	libraryAPI(f, 1, 1)
	mustContain(t, get(t, h, "/library?show=items"),
		`data-play="/items/7/audio"`, `data-title="Bing Crosby – White Christmas"`, `src="/public/static/player.js?v=`, `id="player"`)
}

func TestImportPathsPlayAudioFilesOnly(t *testing.T) {
	f, h := newTest(t)
	importPathsRoute(f)
	rec := get(t, h, "/import?path=/in/incoming")
	mustContain(t, rec, `data-play="/import-paths/audio?path=%2Fin%2Fincoming%2Floose.mp3"`)
	if n := strings.Count(rec.Body.String(), "data-play="); n != 1 {
		t.Errorf("%d play buttons; a folder has none", n)
	}
}

func TestIsAudioFile(t *testing.T) {
	for name, want := range map[string]bool{"a.MP3": true, "b.flac": true, "cover.jpg": false, "noext": false, "x.cue": false} {
		if isAudioFile(name) != want {
			t.Errorf("%s: %v", name, !want)
		}
	}
}
