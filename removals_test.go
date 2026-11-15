package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func removalJSON(status string, extra map[string]any) map[string]any {
	result := map[string]any{"preview": map[string]any{
		"albums": []any{map[string]any{"id": 1, "label": "AC/DC - Back in Black", "fingerprint": "x",
			"items": []any{map[string]any{"id": 11, "path": "/m/AC_DC/Back in Black/01.mp3"}, map[string]any{"id": 12, "path": "/m/AC_DC/Back in Black/02.mp3"}},
			"art":   "/m/AC_DC/Back in Black/cover.jpg"}},
		"items": []any{},
	}}
	for k, v := range extra {
		result[k] = v
	}
	return map[string]any{"id": 9, "status": status, "request": map[string]any{"scope": "album", "query": "id:1"}, "result": result, "created_at": "2026-11-14T10:00:00.123456+00:00"}
}

func TestRemoveSelectedMakesAPreviewAndRemovesNothing(t *testing.T) {
	f, h := newTest(t)
	f.routes["POST /removals"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 201, removalJSON("awaiting_confirmation", nil))
	}
	rec := post(t, h, "/removals", url.Values{"scope": {"albums"}, "id": {"1", "2"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/removals/9" {
		t.Fatalf("got %d %s", rec.Code, rec.Header().Get("Location"))
	}
	_, body := f.last(t, "POST", "/removals")
	var req map[string]string
	json.Unmarshal([]byte(body), &req)
	if req["scope"] != "album" || req["query"] != "id:1 , id:2" {
		t.Errorf("request %v", req)
	}
	for _, r := range f.requests {
		if strings.HasSuffix(r.URL.Path, "/confirm") {
			t.Error("a Preview was confirmed without being asked")
		}
	}
}

func TestRemoveAllUsesTheQueryAndRefusesNothingSelected(t *testing.T) {
	f, h := newTest(t)
	f.routes["POST /removals"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 201, removalJSON("awaiting_confirmation", nil))
	}
	post(t, h, "/removals", url.Values{"scope": {"items"}, "q": {"artist:Radiohead"}})
	_, body := f.last(t, "POST", "/removals")
	if !strings.Contains(body, `"scope":"item"`) || !strings.Contains(body, `"query":"artist:Radiohead"`) {
		t.Errorf("body %s", body)
	}
	n := len(f.requests)
	for _, form := range []url.Values{{"scope": {"items"}}, {"scope": {"things"}, "id": {"1"}}, {"id": {"1"}}} {
		if rec := post(t, h, "/removals", form); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: %d", form, rec.Code)
		}
	}
	if len(f.requests) != n {
		t.Error("a refused form reached beets-api")
	}
}

func TestRemovalPreviewWarnsAboutDeletingFilesBeforeAsking(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /removals/9"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, removalJSON("awaiting_confirmation", nil))
	}
	rec := get(t, h, "/removals/9")
	mustContain(t, rec, "Removal 9", "2026-11-14T10:00:00.123+00:00", "awaiting confirmation",
		"removes 1 Album from the Library and moves 3 files to the Trash", "until the Trash is purged",
		`action="/removals/9/confirm"`, "Remove 1 Album</button>",
		`href="/albums/1"`, "AC/DC - Back in Black", "2 files", "/m/AC_DC/Back in Black/01.mp3", "cover.jpg")
}

func TestRemovalStatuses(t *testing.T) {
	f, h := newTest(t)
	serve := func(status string, extra map[string]any) {
		f.routes["GET /removals/9"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, removalJSON(status, extra)) }
	}
	serve("applied", map[string]any{"removed": []any{map[string]any{"kind": "albums", "id": 1}}})
	rec := get(t, h, "/removals/9")
	mustContain(t, rec, "Removed 1 record", "in folder <code>9</code>", `href="/trash"`, `href="/audit?operation=9"`)
	if strings.Contains(rec.Body.String(), "/confirm") {
		t.Error("an applied Removal can be confirmed again")
	}

	serve("partial", map[string]any{"removed": []any{}, "failed": []any{map[string]any{"kind": "albums", "id": 3, "error": "disk is read-only"}}})
	mustContain(t, get(t, h, "/removals/9"), "1 failed", `href="/albums/3"`, "disk is read-only") // links by the API's `kind`

	serve("rejected", map[string]any{"stale": []any{map[string]any{"kind": "items", "id": 12}}})
	mustContain(t, get(t, h, "/removals/9"), "nothing was removed", `href="/items/12"`, `action="/removals/9/again"`, "Preview again")

	serve("applying", map[string]any{"removed": []any{}, "failed": []any{}})
	mustContain(t, get(t, h, "/removals/9"), `hx-trigger="every 1s"`, "Removing… 0 of 1 done")

	serve("no_changes", nil)
	mustContain(t, get(t, h, "/removals/9"), "nothing to remove")
}

func TestConfirmingARemovalSendsItAndShowsWhereItIs(t *testing.T) {
	f, h := newTest(t)
	f.routes["POST /removals/9/confirm"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 202, removalJSON("queued", nil)) }
	rec := post(t, h, "/removals/9/confirm", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/removals/9" {
		t.Errorf("got %d %s", rec.Code, rec.Header().Get("Location"))
	}
	f.last(t, "POST", "/removals/9/confirm")
	// Stale or already confirmed: the Removal page says which.
	f.routes["POST /removals/9/confirm"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 409, map[string]any{"detail": "library changed since Preview"})
	}
	if rec := post(t, h, "/removals/9/confirm", nil); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/removals/9" {
		t.Errorf("409: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestPreviewingAStaleRemovalAgainUsesTheSameRequest(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /removals/9"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, removalJSON("rejected", nil)) }
	f.routes["POST /removals"] = func(w http.ResponseWriter, r *http.Request) {
		resp := removalJSON("awaiting_confirmation", nil)
		resp["id"] = 10
		writeJSON(w, 201, resp)
	}
	rec := post(t, h, "/removals/9/again", nil)
	if rec.Header().Get("Location") != "/removals/10" {
		t.Errorf("got %s", rec.Header().Get("Location"))
	}
	if _, body := f.last(t, "POST", "/removals"); !strings.Contains(body, `"query":"id:1"`) {
		t.Errorf("body %s", body)
	}
}

func TestRemovalsList(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /removals"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 2, "results": []any{
			map[string]any{"id": 9, "status": "partial", "request": map[string]any{"scope": "album", "query": "id:1"}, "counts": map[string]any{"albums": 2, "items": 0, "removed": 1, "failed": 1}, "created_at": "2026-11-14T10:00:00.123456+00:00"},
			map[string]any{"id": 8, "status": "applied", "request": map[string]any{"scope": "item", "query": "id:5"}, "counts": map[string]any{"albums": 0, "items": 1, "removed": 1, "failed": 0}, "created_at": "2026-11-13T10:00:00+00:00"},
		}})
	}
	mustContain(t, get(t, h, "/removals"), `href="/removals/9"`, "2026-11-14T10:00:00.123+00:00", "2 Albums", "1 failed", "1 Item", "Removals")
}

func TestListsAndRecordsOfferRemove(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /list"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 1, "limit": 100, "offset": 0, "results": []any{map[string]any{"id": 1, "album": "Back in Black", "albumartist": "AC/DC", "year": 1980}}})
	}
	rec := get(t, h, "/library?show=albums&q=artist:AC/DC")
	mustContain(t, rec, `formaction="/removals" formmethod="post"`, "Remove…", "Remove all 1…", `name="q" value="artist:AC/DC"`)
	// With no search, "all" would be the whole Library: neither Modify all nor Remove all is offered.
	rec = get(t, h, "/library")
	mustContain(t, rec, `formaction="/removals" formmethod="post"`)
	if strings.Contains(rec.Body.String(), "Remove all") || strings.Contains(rec.Body.String(), "Modify all") {
		t.Error("offers to change the whole Library")
	}

	f.routes["GET /albums/2"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": 2, "album": "Let It Be", "albumartist": "The Beatles"})
	}
	mustContain(t, get(t, h, "/albums/2"), `<form method="post" action="/removals"`, `name="scope" value="albums"`, `name="id" value="2"`, "Remove…")
}

func TestMenuHasRemovals(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /removals"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 0, "results": []any{}})
	}
	mustContain(t, get(t, h, "/removals"), `class="dropdown-item active" aria-current="page" href="/removals"`, `class="nav-link dropdown-toggle active"`)
}

func TestAuditLinksARemovalAndSummarizesIt(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /audit"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 3, "results": []any{
			// a Removal
			map[string]any{"id": 3, "recorded_at": "2026-11-14T10:00:00Z", "action": "album.remove", "target": "album:1", "operation_id": 9,
				"changes": map[string]any{"removed": map[string]any{"album": map[string]any{"album": "Back in Black"}, "items": []any{}}, "trashed_to": []any{map[string]any{"from": "/a", "to": "/t/9/a"}, map[string]any{"from": "/b", "to": "/t/9/b"}}, "left_in_place": []any{"/elsewhere/c"}}},
			// an Import replacing a duplicate, and an Import being undone: still Imports
			map[string]any{"id": 2, "recorded_at": "2026-11-14T09:00:00Z", "action": "album.remove", "target": "album:4", "operation_id": 7,
				"changes": map[string]any{"removed": map[string]any{"album": "Old"}, "replaced_by": "album:6"}},
			map[string]any{"id": 1, "recorded_at": "2026-11-14T08:00:00Z", "action": "album.remove", "target": "album:5", "operation_id": 6,
				"changes": map[string]any{"removed": map[string]any{"album": map[string]any{"album": "New"}}, "files": []any{}, "undo_of_import": 6}},
		}})
	}
	rec := get(t, h, "/audit")
	mustContain(t, rec, `href="/removals/9"`, "Removal 9", "removed “Back in Black”, moved 2 files to the Trash, left 1 outside the music directory",
		`href="/imports/7"`, `href="/imports/6"`)
	// Filtered to a Removal, the page names it as one.
	rec = get(t, h, "/audit?operation=9")
	mustContain(t, rec, "Only changes made by")
}

func TestOperationKind(t *testing.T) {
	for _, c := range []struct {
		action  string
		changes map[string]any
		want    string
	}{
		{"album.modify", nil, "Modification"},
		{"item.modify", map[string]any{}, "Modification"},
		{"album.import", map[string]any{}, "Import"},
		{"item.remove", map[string]any{"removed": map[string]any{}, "files": []any{}, "left_in_place": []any{}}, "Removal"},
		{"album.remove", map[string]any{"replaced_by": "album:2"}, "Import"},
		{"item.remove", map[string]any{"undo_of_import": 3}, "Import"},
		{"trash.purge", map[string]any{"files": 3}, "Purge"},
		{"art.upload", nil, "Import"},
	} {
		if got := operationKind(auditEntry{Action: c.action, Changes: c.changes}); got != c.want {
			t.Errorf("%s %v: %s, want %s", c.action, c.changes, got, c.want)
		}
	}
}
