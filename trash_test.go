package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func trashRoute(f *fakeAPI, configured bool, folders ...map[string]any) {
	list := make([]any, len(folders))
	for i, x := range folders {
		list[i] = x
	}
	f.routes["GET /trash"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"configured": configured, "folders": list})
	}
}

func folder(name string, op any, files, bytes int, actions ...string) map[string]any {
	acts := make([]any, len(actions))
	for i, a := range actions {
		acts[i] = a
	}
	return map[string]any{"name": name, "files": files, "bytes": bytes, "modified": "2026-11-14T10:00:00.456789+00:00", "fingerprint": "fp-" + name,
		"operation_id": op, "actions": acts, "actors": []any{"alex"}}
}

func TestRemovalPreviewWarnsAndDisablesWhenTheTrashIsNotSetUp(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /removals/9"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, removalJSON("awaiting_confirmation", nil))
	}
	trashRoute(f, false)
	rec := get(t, h, "/removals/9")
	mustContain(t, rec, "The Trash isn't set up", "<code>BEETS_API_TRASH_DIR</code>", `<button class="btn btn-danger" disabled>`)
	trashRoute(f, true)
	rec = get(t, h, "/removals/9")
	if strings.Contains(rec.Body.String(), "isn't set up") || strings.Contains(rec.Body.String(), "disabled") {
		t.Error("warns about a Trash that is set up")
	}
	// If beets-api can't say, don't claim it isn't set up: its refusal will say so.
	delete(f.routes, "GET /trash")
	if strings.Contains(get(t, h, "/removals/9").Body.String(), "isn't set up") {
		t.Error("claims the Trash is missing without knowing")
	}
}

func TestConfirmingWithoutATrashSaysWhyInsteadOfLookingStale(t *testing.T) {
	f, h := newTest(t)
	f.routes["POST /removals/9/confirm"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 409, map[string]any{"detail": "the Trash is not configured: set BEETS_API_TRASH_DIR"})
	}
	rec := post(t, h, "/removals/9/confirm", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("got %d, want the refusal shown", rec.Code)
	}
	mustContain(t, rec, "the Trash is not configured")
	// A stale Preview is still the Removal page's to explain.
	f.routes["POST /removals/9/confirm"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 409, map[string]any{"detail": "library changed since Preview"})
	}
	if rec := post(t, h, "/removals/9/confirm", nil); rec.Code != http.StatusSeeOther {
		t.Errorf("stale: %d", rec.Code)
	}
}

func TestRemoveOldWithoutATrashIsRefusedAndExplained(t *testing.T) {
	f, h := newTest(t)
	op := map[string]any{"id": 7, "status": "waiting_for_choice", "request": map[string]any{"paths": []string{"/in/a"}}, "result": map[string]any{"choices": []any{},
		"prompt": map[string]any{"id": "p1", "kind": "duplicate", "paths": []string{"/in/a"}, "choices": []string{"skip", "remove", "abort"},
			"existing": []any{map[string]any{"id": 4, "albumartist": "AC/DC", "album": "Back in Black"}}}}}
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, op) }
	trashRoute(f, false)
	rec := post(t, h, "/imports/7/choice", url.Values{"prompt_id": {"p1"}, "action": {"remove"}})
	mustContain(t, rec, "moves their files to the Trash", "The Trash isn't set up", `name="action" value="remove" disabled`)
	f.routes["POST /imports/7/choice"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 409, map[string]any{"detail": "the Trash is not configured: set BEETS_API_TRASH_DIR"})
	}
	rec = post(t, h, "/imports/7/choice", url.Values{"prompt_id": {"p1"}, "action": {"remove"}, "confirm": {"1"}})
	if rec.Code != http.StatusConflict {
		t.Errorf("got %d, want the refusal shown, not a redirect to a stale prompt", rec.Code)
	}
	mustContain(t, rec, "the Trash is not configured")
}

func TestTrashPageListsFoldersAndOffersPurges(t *testing.T) {
	f, h := newTest(t)
	trashRoute(f, true, folder("9", 9, 11, 3250585, "album.remove"), folder("art-album-2-20261114T093000", nil, 1, 150000))
	rec := get(t, h, "/trash")
	mustContain(t, rec, "Trash", "2 folders · 3.2 MB", `name="folder" value="9"`, `href="/audit?operation=9"`, "album.remove", "alex",
		"3.1 MB", "2026-11-14T10:00:00.456+00:00", "Art changed on its own", `value="art-album-2-20261114T093000"`,
		"Purge selected…", "Purge everything older than", `name="older_than_days"`, "Purge everything…", `href="/purges"`, "Nothing is deleted until you confirm")
}

func TestTrashPageStatesAreEmptyAndNotSetUp(t *testing.T) {
	f, h := newTest(t)
	trashRoute(f, true)
	mustContain(t, get(t, h, "/trash"), "The Trash is empty")
	trashRoute(f, false)
	rec := get(t, h, "/trash")
	mustContain(t, rec, "The Trash isn't set up", "BEETS_API_TRASH_DIR")
	if strings.Contains(rec.Body.String(), "Purge selected") {
		t.Error("offers a Purge with no Trash")
	}
}

func TestPurgeRequestsAskForAPreviewAndDeleteNothing(t *testing.T) {
	f, h := newTest(t)
	f.routes["POST /purges"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 201, purgeJSON("awaiting_confirmation", nil))
	}
	body := func() map[string]any {
		_, b := f.last(t, "POST", "/purges")
		var m map[string]any
		json.Unmarshal([]byte(b), &m)
		return m
	}
	rec := post(t, h, "/purges", url.Values{"folder": {"9", "10"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/purges/5" {
		t.Fatalf("got %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if m := body(); len(m["folders"].([]any)) != 2 || m["older_than"] != nil {
		t.Errorf("selected: %v", m)
	}
	post(t, h, "/purges", url.Values{"older_than_days": {"30"}})
	if m := body(); m["folders"] != nil || !strings.HasSuffix(m["older_than"].(string), "Z") {
		t.Errorf("older: %v", m)
	}
	post(t, h, "/purges", url.Values{"all": {"1"}})
	if m := body(); len(m) != 0 {
		t.Errorf("everything: %v", m)
	}
	n := len(f.requests)
	for _, form := range []url.Values{{}, {"older_than_days": {"soon"}}, {"older_than_days": {"-1"}}} {
		if rec := post(t, h, "/purges", form); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: %d", form, rec.Code)
		}
	}
	if len(f.requests) != n {
		t.Error("a refused form reached beets-api")
	}
	for _, r := range f.requests {
		if strings.HasSuffix(r.URL.Path, "/confirm") {
			t.Error("a Preview was confirmed without being asked")
		}
	}
}

func purgeJSON(status string, extra map[string]any) map[string]any {
	result := map[string]any{"preview": map[string]any{"folders": []any{folder("9", 9, 11, 3250585, "album.remove"), folder("10", 10, 1, 1024)}}, "trash_configured": true}
	for k, v := range extra {
		result[k] = v
	}
	return map[string]any{"id": 5, "status": status, "request": map[string]any{"older_than": "2026-11-07T10:00:00Z"}, "result": result, "created_at": "2026-11-14T10:00:00.123456+00:00"}
}

func TestPurgePreviewSaysItCannotBeUndoneBeforeAsking(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /purges/5"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, purgeJSON("awaiting_confirmation", nil))
	}
	trashRoute(f, true)
	rec := get(t, h, "/purges/5")
	mustContain(t, rec, "Purge 5", "2026-11-14T10:00:00.123+00:00", "Everything older than 2026-11-07T10:00:00.000+00:00",
		"Confirming deletes 12 files (3.1 MB) from 2 folders for good.", "This can't be undone", `action="/purges/5/confirm"`,
		"Delete 12 files for good", `class="font-monospace small">9</td>`)
	trashRoute(f, false)
	mustContain(t, get(t, h, "/purges/5"), "The Trash isn't set up", `<button class="btn btn-danger" disabled>`)
}

func TestPurgeStatuses(t *testing.T) {
	f, h := newTest(t)
	serve := func(status string, extra map[string]any) {
		f.routes["GET /purges/5"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, purgeJSON(status, extra)) }
	}
	serve("applied", map[string]any{"purged": []any{"9", "10"}})
	rec := get(t, h, "/purges/5")
	mustContain(t, rec, "Purged 2 folders", `href="/audit?operation=5"`)
	if strings.Contains(rec.Body.String(), "/confirm") {
		t.Error("an applied Purge can be confirmed again")
	}
	serve("partial", map[string]any{"purged": []any{"9"}, "failed": []any{map[string]any{"name": "10", "error": "permission denied"}}})
	mustContain(t, get(t, h, "/purges/5"), "1 failed", "permission denied")
	serve("rejected", map[string]any{"stale": []any{"9"}})
	mustContain(t, get(t, h, "/purges/5"), "The Trash changed after this Preview", `action="/purges/5/again"`, "Preview again")
	serve("applying", map[string]any{"purged": []any{}, "failed": []any{}})
	mustContain(t, get(t, h, "/purges/5"), `hx-trigger="every 1s"`, "Purging… 0 of 2 folders done")
	serve("no_changes", nil)
	mustContain(t, get(t, h, "/purges/5"), "nothing to purge")
}

func TestConfirmingAPurge(t *testing.T) {
	f, h := newTest(t)
	f.routes["POST /purges/5/confirm"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 202, purgeJSON("queued", nil)) }
	if rec := post(t, h, "/purges/5/confirm", nil); rec.Header().Get("Location") != "/purges/5" {
		t.Errorf("got %s", rec.Header().Get("Location"))
	}
	f.last(t, "POST", "/purges/5/confirm")
	f.routes["POST /purges/5/confirm"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 409, map[string]any{"detail": "the Trash is not configured: set BEETS_API_TRASH_DIR"})
	}
	if rec := post(t, h, "/purges/5/confirm", nil); rec.Code != http.StatusConflict {
		t.Errorf("no Trash: %d", rec.Code)
	}
	f.routes["POST /purges/5/confirm"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 409, map[string]any{"detail": "the Trash changed since Preview"})
	}
	// "the Trash changed" is a stale Preview, not a missing Trash, but also says "Trash":
	// the page is where it is explained, so it must still be told apart.
	if rec := post(t, h, "/purges/5/confirm", nil); rec.Code != http.StatusSeeOther {
		t.Errorf("stale: %d, want the Purge page to explain", rec.Code)
	}
}

func TestPurgeAgainUsesTheSameRequestAndPurgesList(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /purges/5"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, purgeJSON("rejected", nil)) }
	f.routes["POST /purges"] = func(w http.ResponseWriter, r *http.Request) {
		resp := purgeJSON("awaiting_confirmation", nil)
		resp["id"] = 6
		writeJSON(w, 201, resp)
	}
	rec := post(t, h, "/purges/5/again", nil)
	if rec.Header().Get("Location") != "/purges/6" {
		t.Errorf("got %s", rec.Header().Get("Location"))
	}
	if _, body := f.last(t, "POST", "/purges"); !strings.Contains(body, "2026-11-07T10:00:00Z") {
		t.Errorf("body %s", body)
	}
	f.routes["GET /purges"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 1, "results": []any{map[string]any{"id": 5, "status": "applied", "request": map[string]any{"folders": []any{"9"}},
			"counts": map[string]any{"folders": 1, "purged": 1, "failed": 0}, "created_at": "2026-11-14T10:00:00.123456+00:00"}}})
	}
	mustContain(t, get(t, h, "/purges"), `href="/purges/5"`, "Folders 9", "1 folder", "Purges")
}

func TestAuditShowsPurgesAndWhereFilesWent(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /audit"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 1, "results": []any{
			map[string]any{"id": 7, "recorded_at": "2026-11-14T10:00:00Z", "action": "trash.purge", "target": "trash:9", "operation_id": 5, "actor": "alex",
				"changes": map[string]any{"files": 12, "bytes": 3250585}}}})
	}
	rec := get(t, h, "/audit")
	mustContain(t, rec, "trash.purge", "purged 12 files · 3.1 MB", `href="/purges/5"`, "Purge 5", `href="/trash"`, `<option value="trash.purge"`)
	f.routes["GET /audit/8"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": 8, "recorded_at": "2026-11-14T10:00:00Z", "action": "album.remove", "target": "album:1", "operation_id": 9,
			"changes": map[string]any{"removed": map[string]any{"album": "X"}, "left_in_place": []any{},
				"trashed_to": []any{map[string]any{"from": "/m/a.mp3", "to": "/t/9/a.mp3"}, map[string]any{"from": "/m/b.mp3", "to": "/t/9/b.mp3"}}}})
	}
	mustContain(t, get(t, h, "/audit/8"), "2 files moved to the Trash", "/m/a.mp3", "/t/9/a.mp3", "/t/9/b.mp3")
}

func TestMenuHasTrash(t *testing.T) {
	f, h := newTest(t)
	trashRoute(f, true)
	mustContain(t, get(t, h, "/trash"), `class="dropdown-item active" aria-current="page" href="/trash"`)
}
