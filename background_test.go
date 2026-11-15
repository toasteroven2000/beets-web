package main

import (
	"net/http"
	"strings"
	"testing"
)

// beets-api builds Previews in the background: a new one is "previewing" until it is ready.

func TestModificationPreviewIsFollowedWhileItIsBuilt(t *testing.T) {
	f, h := newTest(t)
	mod := func(status string, result map[string]any) {
		f.routes["GET /modifications/7"] = func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]any{"id": 7, "status": status, "request": map[string]any{"scope": "item", "query": "id:1", "set": map[string]any{"year": "1970"}},
				"result": result, "created_at": "2026-11-15T10:00:00Z"})
		}
	}
	mod("previewing", map[string]any{"write": true, "move": true})
	rec := get(t, h, "/modifications/7")
	mustContain(t, rec, "Building the Preview…", `hx-trigger="every 1s"`, "previewing")
	if strings.Contains(rec.Body.String(), "/confirm") {
		t.Error("a Preview that isn't built can be confirmed")
	}
	mod("failed", map[string]any{"error": "no such field"})
	mustContain(t, get(t, h, "/modifications/7"), "The Preview failed: no such field", `action="/modifications/7/again"`, "Preview again")
	mod("interrupted", map[string]any{"write": true, "move": true})
	mustContain(t, get(t, h, "/modifications/7"), "restarted before the Preview was ready")
}

func TestRemovalPreviewIsFollowedWhileItIsBuilt(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /removals/9"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": 9, "status": "previewing", "request": map[string]any{"scope": "album", "query": "id:1"}, "result": map[string]any{}, "created_at": "2026-11-15T10:00:00Z"})
	}
	mustContain(t, get(t, h, "/removals/9"), "Building the Preview…", `hx-trigger="every 1s"`)
	f.routes["GET /removals/9"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, removalJSON("failed", map[string]any{"error": "boom"}))
	}
	mustContain(t, get(t, h, "/removals/9"), "The Preview failed: boom", `action="/removals/9/again"`)
	f.routes["GET /removals/9"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": 9, "status": "interrupted", "request": map[string]any{"scope": "album", "query": "id:1"}, "result": map[string]any{}, "created_at": "x"})
	}
	mustContain(t, get(t, h, "/removals/9"), "restarted before the Preview was ready")
}

func TestPurgePreviewIsFollowedWhileItIsBuilt(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /purges/5"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": 5, "status": "previewing", "request": map[string]any{}, "result": map[string]any{}, "created_at": "2026-11-15T10:00:00Z"})
	}
	mustContain(t, get(t, h, "/purges/5"), "Building the Preview…", `hx-trigger="every 1s"`)
	f.routes["GET /purges/5"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, purgeJSON("failed", map[string]any{"error": "disk"}))
	}
	mustContain(t, get(t, h, "/purges/5"), "The Preview failed: disk", `action="/purges/5/again"`)
}

func TestStatusesOfBackgroundWork(t *testing.T) {
	if !inProgress("previewing") || inProgress("awaiting_confirmation") || inProgress("failed") {
		t.Error("inProgress")
	}
	if statusClass("previewing") != "text-bg-info" || statusClass("failed") != "text-bg-danger" {
		t.Error("statusClass")
	}
}

// Folders are paged now: a big one comes in pages of 200.

func importPathsPage(f *fakeAPI, total, offset, shown int) {
	f.routes["GET /import-paths"] = func(w http.ResponseWriter, r *http.Request) {
		entries := []any{}
		for i := 0; i < shown; i++ {
			entries = append(entries, map[string]any{"name": "dir" + string(rune('a'+i%26)), "path": "/in/x", "type": "dir", "mtime": "2026-11-15T10:00:00", "size": nil})
		}
		writeJSON(w, 200, map[string]any{"path": "/in/big", "parent": "/in", "total": total, "limit": 200, "offset": offset, "entries": entries})
	}
}

func TestBigFolderIsPagedThrough(t *testing.T) {
	f, h := newTest(t)
	importPathsPage(f, 450, 0, 200)
	rec := get(t, h, "/import?path=/in/big")
	mustContain(t, rec, "1–200 of 450", "Later", `href="/import?offset=200&amp;path=%2Fin%2Fbig"`)
	if strings.Contains(rec.Body.String(), "Earlier") {
		t.Error("Earlier on the first page")
	}
	if r, _ := f.last(t, "GET", "/import-paths"); r.URL.Query().Get("limit") != "200" || r.URL.Query().Get("offset") != "0" {
		t.Errorf("asked for %v", r.URL.Query())
	}
	importPathsPage(f, 450, 200, 200)
	rec = get(t, h, "/import?path=/in/big&offset=200")
	mustContain(t, rec, "201–400 of 450", "Earlier", "Later", `href="/import?path=%2Fin%2Fbig"`, `href="/import?offset=400&amp;path=%2Fin%2Fbig"`)
	if r, _ := f.last(t, "GET", "/import-paths"); r.URL.Query().Get("offset") != "200" {
		t.Errorf("asked for %v", r.URL.Query())
	}
	importPathsPage(f, 450, 400, 50)
	rec = get(t, h, "/import?path=/in/big&offset=400")
	mustContain(t, rec, "401–450 of 450", "Earlier")
	if strings.Contains(rec.Body.String(), "Later") {
		t.Error("Later on the last page")
	}
}

func TestSmallFolderHasNoPaging(t *testing.T) {
	f, h := newTest(t)
	importPathsPage(f, 3, 0, 3)
	rec := get(t, h, "/import?path=/in/big")
	if strings.Contains(rec.Body.String(), "Earlier") || strings.Contains(rec.Body.String(), "Later") || strings.Contains(rec.Body.String(), " of 3") {
		t.Error("paging controls for a folder that fits on a page")
	}
}
