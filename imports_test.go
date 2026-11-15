package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func importPathsRoute(f *fakeAPI) {
	f.routes["GET /import-paths"] = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("path") == "" {
			writeJSON(w, 200, map[string]any{"path": nil, "parent": nil, "entries": []any{
				map[string]any{"name": "incoming", "path": "/in/incoming", "type": "dir", "mtime": "2026-11-09T10:00:00+00:00"},
			}})
			return
		}
		writeJSON(w, 200, map[string]any{"path": "/in/incoming", "parent": nil, "entries": []any{
			map[string]any{"name": "Abbey Road", "path": "/in/incoming/Abbey Road", "type": "dir", "mtime": "2026-11-09T10:00:00+00:00"},
			map[string]any{"name": "loose.mp3", "path": "/in/incoming/loose.mp3", "type": "file", "mtime": "2026-11-09T10:00:00+00:00", "size": 3145728},
		}})
	}
}

func TestImportBrowsesImportPaths(t *testing.T) {
	f, h := newTest(t)
	importPathsRoute(f)
	mustContain(t, get(t, h, "/import"), `href="/import?path=%2Fin%2Fincoming"`, "Import Paths", ">Import<")
	rec := get(t, h, "/import?path=/in/incoming")
	mustContain(t, rec,
		`name="path" value="/in/incoming/Abbey Road"`, // one Import button per folder
		`name="path" value="/in/incoming"><i class="bi bi-box-arrow-in-down" aria-hidden="true"></i> Import this folder`,
		`href="/import"><i class="bi bi-arrow-up" aria-hidden="true"></i> Up`, "3.0 MB")
	if strings.Contains(rec.Body.String(), `value="/in/incoming/loose.mp3"`) {
		t.Error("files get an Import button; only folders should")
	}
}

func TestImportModeFromConfig(t *testing.T) {
	cases := []struct {
		imp  map[string]any
		want string
		warn bool
	}{
		{map[string]any{"copy": true, "move": true}, "moved", true},
		{map[string]any{"copy": true, "delete": true}, "then the originals deleted", true},
		{map[string]any{"copy": true}, "copied", false},
		{map[string]any{"hardlink": true}, "linked", false},
		{map[string]any{}, "in place", false},
	}
	for _, c := range cases {
		m := importModeFrom(map[string]any{"import": c.imp})
		if !strings.Contains(m.Text, c.want) || m.Warn != c.warn {
			t.Errorf("%v: got %+v, want %q warn=%v", c.imp, m, c.want, c.warn)
		}
	}
}

func TestImportWithoutImportPathsSaysSo(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /import-paths"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"path": nil, "parent": nil, "entries": []any{}})
	}
	mustContain(t, get(t, h, "/import"), "BEETS_API_IMPORT_PATHS")
}

func TestStartImportOfOneFolder(t *testing.T) {
	f, h := newTest(t)
	f.routes["POST /imports"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 202, map[string]any{"id": 7, "status": "running", "request": map[string]any{"paths": []string{"/in/a"}}})
	}
	rec := post(t, h, "/imports", url.Values{"path": {"/in/a"}, "singletons": {"1"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/imports/7" {
		t.Fatalf("%d %s", rec.Code, rec.Header().Get("Location"))
	}
	r, body := f.last(t, "POST", "/imports")
	var got importRequest
	json.Unmarshal([]byte(body), &got)
	if len(got.Paths) != 1 || got.Paths[0] != "/in/a" || !got.Singletons {
		t.Errorf("body %s", body)
	}
	if r.Header.Get("X-Beets-API-Client") != "beets-ui" {
		t.Error("no X-Beets-API-Client")
	}
}

func TestStartImportWhileOneIsActiveLinksToIt(t *testing.T) {
	f, h := newTest(t)
	importPathsRoute(f)
	f.routes["POST /imports"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 409, map[string]any{"detail": "import 4 is still active"})
	}
	rec := post(t, h, "/imports", url.Values{"path": {"/in/incoming/Abbey Road"}, "here": {"/in/incoming"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status %d", rec.Code)
	}
	mustContain(t, rec, "Import 4 is still active", `href="/imports/4"`, `value="/in/incoming/Abbey Road"`)
}

func importsRoute(f *fakeAPI) {
	f.routes["GET /imports"] = func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case len(q["status"]) == 2: // the active Import
			writeJSON(w, 200, map[string]any{"total": 1, "results": []any{
				map[string]any{"id": 9, "status": "waiting_for_choice", "request": map[string]any{"paths": []string{"/in/b"}}},
			}})
		case q.Get("status") == "failed":
			writeJSON(w, 200, map[string]any{"total": 0, "results": []any{}})
		default:
			writeJSON(w, 200, map[string]any{"total": 120, "results": []any{
				map[string]any{"id": 8, "status": "completed", "request": map[string]any{"paths": []string{"/in/a"}}, "created_at": "2026-11-09T09:00:00Z",
					"counts": map[string]any{"choices": 3, "tasks": 3, "skipped": 1, "items": 24}},
				map[string]any{"id": 7, "status": "aborted", "request": map[string]any{"paths": []string{"/in/c"}}, "created_at": "2026-11-08T09:00:00Z",
					"counts": map[string]any{"choices": 1, "tasks": 0, "skipped": 0, "items": 0}},
			}})
		}
	}
}

func TestImportHistoryTab(t *testing.T) {
	f, h := newTest(t)
	importsRoute(f)
	rec := get(t, h, "/import?tab=history&offset=50")
	mustContain(t, rec, "Import 9 is waiting for choice", `href="/imports/9"`, `href="/imports/8"`, "2026-11-09 09:00",
		"24 Items imported · 1 of 3 Import Tasks skipped", "Nothing imported", "120 Imports",
		`aria-current="page" href="/import?tab=history"`, `<span class="badge text-bg-secondary">120</span>`,
		`href="/import?tab=history"><i class="bi bi-chevron-left" aria-hidden="true"></i> Newer`,
		`href="/import?offset=100&amp;tab=history">Older`)
	for _, r := range f.requests {
		if r.URL.Path == "/import-paths" {
			t.Error("History tab browsed the Import Paths")
		}
	}
}

func TestImportHistoryFiltersByStatus(t *testing.T) {
	f, h := newTest(t)
	importsRoute(f)
	rec := get(t, h, "/import?tab=history&status=failed")
	mustContain(t, rec, "No failed Imports.", `<option value="failed" selected>`, `<span class="badge text-bg-secondary">120</span>`, `href="/import?tab=history">Clear`)
}

func TestImportPathsTabCountsHistory(t *testing.T) {
	f, h := newTest(t)
	importPathsRoute(f)
	importsRoute(f)
	rec := get(t, h, "/import?path=/in/incoming")
	mustContain(t, rec, `aria-current="page" href="/import?path=%2Fin%2Fincoming"`, `<span class="badge text-bg-secondary">120</span>`, "Abbey Road")
	if strings.Contains(rec.Body.String(), "2026-11-09 09:00") {
		t.Error("Import Paths tab lists the History")
	}
}

func TestRunningImportPolls(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": 7, "status": "running", "request": map[string]any{"paths": []string{"/in/a"}}, "result": map[string]any{}})
	}
	mustContain(t, get(t, h, "/imports/7"), `hx-trigger="every 1s"`, "beets is working", `action="/imports/7/abort"`)
}

func albumPrompt() map[string]any {
	return map[string]any{"id": 7, "status": "waiting_for_choice", "request": map[string]any{"paths": []string{"/in/a"}},
		"result": map[string]any{"choices": []any{}, "prompt": map[string]any{
			"id": "p1", "kind": "album", "paths": []string{"/in/a"},
			"choices":        []string{"apply", "asis", "skip", "tracks", "albums", "search", "abort"},
			"current":        map[string]any{"artist": "Beatles", "album": "Abbey Rd"},
			"recommendation": "medium",
			"candidates": []any{map[string]any{
				"index": 0, "distance": 0.08, "artist": "The Beatles", "album": "Abbey Road", "year": 1969, "id": "mb-1", "data_source": "MusicBrainz",
				"tracks": []any{map[string]any{"path": "/in/a/01.mp3",
					"changes": map[string]any{
						"title":      map[string]any{"old": "come together", "new": "Come Together"},
						"year":       map[string]any{"old": 1970, "new": 1969},
						"mb_albumid": map[string]any{"old": nil, "new": "mb-1"},
					},
					"fields": map[string]any{
						"title":  map[string]any{"old": "come together", "new": "Come Together"},
						"track":  map[string]any{"old": 1, "new": 1},
						"length": map[string]any{"old": 259.9, "new": 259.0},
						"label":  map[string]any{"old": "Apple", "new": "Apple"},
						"year":   map[string]any{"old": 1970, "new": 1969},
						"month":  map[string]any{"old": 0, "new": 9},
						"day":    map[string]any{"old": 0, "new": 26},
					}}},
				"extra_items": []any{}, "extra_tracks": []any{"Her Majesty"},
			}},
		}}}
}

func TestAlbumPromptShowsCandidatesAndChoices(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, albumPrompt()) }
	f.routes["GET /config"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"import": map[string]any{"copy": true}})
	}
	rec := get(t, h, "/imports/7")
	mustContain(t, rec, "92%", "copied into the Library; the originals stay", // beside Apply
		"MusicBrainz · 1 of 2 change", "The Beatles – Abbey Road", "1969", `href="https://musicbrainz.org/release/mb-1"`, "musicbrainz.svg?v=",
		"<del>1970</del>", "<ins>1969-09-26</ins>", // changed: old → new
		"<td>Apple</td>", // unchanged: just the value
		"<del>come together</del>", "<ins>Come Together</ins>", "4:20",
		"1 more fields Apply also sets (mb_albumid)", "<code>mb_albumid</code>", "Her Majesty",
		`name="action" value="apply"`, `name="candidate" value="0"`, `name="prompt_id" value="p1"`,
		">Use as-is<", ">Import as tracks<", ">Group albums<", ">Skip<", "Search again", `value="Abbey Rd"`, "medium")
	if strings.Contains(rec.Body.String(), `hx-trigger`) {
		t.Error("polls while waiting for a Choice")
	}
}

func TestCandidateListsEveryTrackByDiscWithArrowsOnlyOnChanges(t *testing.T) {
	f, h := newTest(t)
	track := func(disc, n int, old, new string) map[string]any {
		return map[string]any{"path": "/in/a/x.mp3", "changes": map[string]any{}, "fields": map[string]any{
			"disc": map[string]any{"old": disc, "new": disc}, "track": map[string]any{"old": n, "new": n},
			"title": map[string]any{"old": old, "new": new}, "length": map[string]any{"old": 61.0, "new": 61.0}}}
	}
	prompt := albumPrompt()
	cand := prompt["result"].(map[string]any)["prompt"].(map[string]any)["candidates"].([]any)[0].(map[string]any)
	cand["tracks"] = []any{track(1, 1, "Same One", "Same One"), track(1, 2, "sloppy two", "Sloppy Two"), track(2, 1, "Same Three", "Same Three")}
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, prompt) }
	f.routes["GET /config"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"import": map[string]any{"move": true}})
	}
	rec := get(t, h, "/imports/7")
	mustContain(t, rec, "Disc 1 · 2 tracks · 1 changed", "Disc 2 · 1 track · all match", "Same One", "Same Three",
		"<del>sloppy two</del>", "<ins>Sloppy Two</ins>", "originals are deleted") // a move says so by Apply
	if strings.Count(rec.Body.String(), "<del>") != 1 {
		t.Errorf("only the one changed title should have an old value shown:\n%s", rec.Body.String())
	}
}

func TestApplyCandidateSendsChoice(t *testing.T) {
	f, h := newTest(t)
	f.routes["POST /imports/7/choice"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 202, map[string]any{"id": 7}) }
	rec := post(t, h, "/imports/7/choice", url.Values{"prompt_id": {"p1"}, "action": {"apply"}, "candidate": {"0"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/imports/7" {
		t.Fatalf("%d %s", rec.Code, rec.Header().Get("Location"))
	}
	_, body := f.last(t, "POST", "/imports/7/choice")
	var got choiceRequest
	json.Unmarshal([]byte(body), &got)
	if got.PromptID != "p1" || got.Action != "apply" || got.Candidate == nil || *got.Candidate != 0 {
		t.Errorf("body %s", body)
	}
}

func TestSearchSendsIDsOrArtistAndName(t *testing.T) {
	f, h := newTest(t)
	f.routes["POST /imports/7/choice"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 202, map[string]any{"id": 7}) }
	post(t, h, "/imports/7/choice", url.Values{"prompt_id": {"p1"}, "action": {"search"}, "artist": {"x"}, "name": {"y"}, "search_ids": {"a, b"}})
	_, body := f.last(t, "POST", "/imports/7/choice")
	var got choiceRequest
	json.Unmarshal([]byte(body), &got)
	if strings.Join(got.SearchIDs, ",") != "a,b" || got.Artist != nil {
		t.Errorf("ids: %s", body)
	}
	post(t, h, "/imports/7/choice", url.Values{"prompt_id": {"p1"}, "action": {"search"}, "artist": {" The Beatles "}, "name": {"Abbey Road"}})
	_, body = f.last(t, "POST", "/imports/7/choice")
	got = choiceRequest{}
	json.Unmarshal([]byte(body), &got)
	if got.Artist == nil || *got.Artist != "The Beatles" || *got.Name != "Abbey Road" || len(got.SearchIDs) != 0 {
		t.Errorf("artist and name: %s", body)
	}
}

func duplicatePrompt() map[string]any {
	return map[string]any{"id": 7, "status": "waiting_for_choice", "request": map[string]any{"paths": []string{"/in/a"}},
		"result": map[string]any{"prompt": map[string]any{
			"id": "p2", "kind": "duplicate", "paths": []string{"/in/a"}, "choices": []string{"skip", "keep", "remove", "merge", "abort"},
			"existing": []any{map[string]any{"id": 3, "albumartist": "The Beatles", "album": "Abbey Road"}},
		}}}
}

func TestDuplicatePromptLinksExistingAlbums(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, duplicatePrompt()) }
	mustContain(t, get(t, h, "/imports/7"), "Already in the Library", `href="/albums/3"`, "The Beatles – Abbey Road",
		">Skip<", ">Keep both<", ">Merge<", ">Remove old<", "moving their files to the Trash")
}

func TestRemoveOldAsksFirst(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, duplicatePrompt()) }
	f.routes["POST /imports/7/choice"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 202, map[string]any{"id": 7}) }
	rec := post(t, h, "/imports/7/choice", url.Values{"prompt_id": {"p2"}, "action": {"remove"}})
	mustContain(t, rec, "Remove these from the Library?", `href="/albums/3"`, `name="confirm" value="1"`)
	for _, r := range f.requests {
		if r.Method == "POST" {
			t.Fatal("sent remove without confirming")
		}
	}
	rec = post(t, h, "/imports/7/choice", url.Values{"prompt_id": {"p2"}, "action": {"remove"}, "confirm": {"1"}})
	if rec.Code != http.StatusSeeOther {
		t.Errorf("status %d", rec.Code)
	}
	if _, body := f.last(t, "POST", "/imports/7/choice"); !strings.Contains(body, `"action":"remove"`) {
		t.Errorf("body %s", body)
	}
}

func TestStaleChoiceShowsWhereTheImportIs(t *testing.T) {
	f, h := newTest(t)
	f.routes["POST /imports/7/choice"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 409, map[string]any{"detail": "this prompt is no longer current"})
	}
	rec := post(t, h, "/imports/7/choice", url.Values{"prompt_id": {"old"}, "action": {"skip"}})
	if rec.Header().Get("Location") != "/imports/7?stale=1" {
		t.Errorf("location %q", rec.Header().Get("Location"))
	}
}

func TestAbortImport(t *testing.T) {
	f, h := newTest(t)
	f.routes["DELETE /imports/7"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 202, map[string]any{"id": 7}) }
	rec := post(t, h, "/imports/7/abort", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/imports/7" {
		t.Errorf("%d %s", rec.Code, rec.Header().Get("Location"))
	}
	f.last(t, "DELETE", "/imports/7")
}

func TestFinishedImportLinksWhatItMade(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": 7, "status": "completed", "request": map[string]any{"paths": []string{"/in"}},
			"result": map[string]any{
				"choices": []any{map[string]any{"paths": []string{"/in/a"}, "kind": "album", "action": "apply", "candidate": 0}},
				"outcomes": []any{
					map[string]any{"paths": []string{"/in/a"}, "skipped": false, "items": []any{map[string]any{"id": 11, "album_id": 3}, map[string]any{"id": 12, "album_id": 3}}},
					map[string]any{"paths": []string{"/in/b"}, "skipped": true, "items": []any{}},
					map[string]any{"paths": []string{"/in/c"}, "skipped": false, "items": []any{}}, // aborted mid-Task
				}}})
	}
	rec := get(t, h, "/imports/7")
	// The Album and each of its Items, so either can be followed.
	mustContain(t, rec, `href="/albums/3"`, "Album 3", `href="/items/11"`, `href="/items/12"`, "Item 11", "Item 12", "Skipped", "Nothing imported", `href="/audit?operation=7"`,
		`href="/imports/7/undo"`)
	if strings.Contains(rec.Body.String(), "Abort Import") {
		t.Error("finished Import offers Abort")
	}
}

func finishedImport(extra map[string]any) map[string]any {
	result := map[string]any{"choices": []any{}, "outcomes": []any{
		map[string]any{"paths": []string{"/in/a"}, "skipped": false, "items": []any{map[string]any{"id": 11, "album_id": 3}}}}}
	for k, v := range extra {
		result[k] = v
	}
	return map[string]any{"id": 7, "status": "completed", "request": map[string]any{"paths": []string{"/in"}}, "result": result}
}

func TestUndoneImportSaysSoInsteadOfOfferingUndo(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, finishedImport(map[string]any{"undone": map[string]any{"at": "2026-11-10T09:00:00+00:00"}}))
	}
	rec := get(t, h, "/imports/7")
	mustContain(t, rec, "Undone")
	if strings.Contains(rec.Body.String(), "/imports/7/undo") {
		t.Error("offers to undo an Import that was undone")
	}
}

func TestUndoPreviewAsksBeforeDeletingFiles(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, finishedImport(nil)) }
	f.routes["GET /imports/7/undo"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"import_id": 7, "mode": "copy", "allowed": true, "reasons": []string{},
			"albums": []any{map[string]any{"target": "album:3", "id": 3, "albumartist": "AC/DC", "album": "Back in Black",
				"items": []any{map[string]any{"id": 11, "path": "/lib/AC_DC/Back in Black/01.mp3"}}, "art": "/lib/AC_DC/Back in Black/cover.jpg"}},
			"items": []any{}, "changes_since": []any{}})
	}
	rec := get(t, h, "/imports/7/undo")
	mustContain(t, rec, "Undo Import 7?", "files it copied in", "originals in the Import Path stay", `href="/albums/3"`, "AC/DC – Back in Black",
		"/lib/AC_DC/Back in Black/01.mp3", "cover.jpg", `action="/imports/7/undo"`, "Remove and delete files")
}

func TestUndoPreviewExplainsWhyItCannotBeUndone(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, finishedImport(nil)) }
	f.routes["GET /imports/7/undo"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"import_id": 7, "mode": "copy", "allowed": false,
			"reasons": []string{"1 later changes were made to what the Import added"},
			"albums":  []any{}, "items": []any{},
			"changes_since": []any{map[string]any{"id": 9, "recorded_at": "2026-11-10T08:00:00+00:00", "action": "album.modify", "target": "album:3"}}})
	}
	rec := get(t, h, "/imports/7/undo")
	mustContain(t, rec, "can&#39;t be undone", "1 later changes were made", "album.modify", `href="/albums/3"`, `href="/audit?operation=7"`)
	if strings.Contains(rec.Body.String(), "Remove and delete files") {
		t.Error("offers to undo an Import that may not be undone")
	}
}

func TestConfirmingUndoShowsTheImportWhileItRunsInTheBackground(t *testing.T) {
	f, h := newTest(t)
	f.routes["POST /imports/7/undo"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 202, map[string]any{}) }
	rec := post(t, h, "/imports/7/undo", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/imports/7" {
		t.Errorf("got %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAnUndoAlreadyRunningIsSaidNotHiddenBehindThePreview(t *testing.T) {
	f, h := newTest(t)
	f.routes["POST /imports/7/undo"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 409, map[string]any{"detail": "an undo of import 5 is still running"})
	}
	rec := post(t, h, "/imports/7/undo", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("got %d", rec.Code)
	}
	mustContain(t, rec, "an undo of import 5 is still running")
}

func TestImportPageFollowsAnUndoUntilItFinishes(t *testing.T) {
	f, h := newTest(t)
	op := finishedImport(nil)
	op["undoing"] = true
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, op) }
	rec := get(t, h, "/imports/7")
	mustContain(t, rec, "Undoing this Import", `hx-trigger="every 1s"`)
	if strings.Contains(rec.Body.String(), "Undo this Import…") {
		t.Error("offers to undo an Import that is being undone")
	}
	op["undoing"] = false
	op["result"].(map[string]any)["undone"] = map[string]any{"at": "2026-11-15T10:00:00+00:00"}
	rec = get(t, h, "/imports/7")
	mustContain(t, rec, "Undone")
	if strings.Contains(rec.Body.String(), "every 1s") || strings.Contains(rec.Body.String(), "Undoing this Import") {
		t.Error("still following an undo that finished")
	}
	delete(op["result"].(map[string]any), "undone")
	op["result"].(map[string]any)["undo_error"] = "the Trash is not configured"
	mustContain(t, get(t, h, "/imports/7"), "The undo didn't finish: the Trash is not configured")
}

func TestUndoRefusedAfterThePreviewGoesBackToThePreview(t *testing.T) {
	f, h := newTest(t)
	f.routes["POST /imports/7/undo"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 409, map[string]any{"detail": "cannot undo this Import: it was already undone"})
	}
	rec := post(t, h, "/imports/7/undo", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/imports/7/undo" {
		t.Errorf("got %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAuditFiltersByImport(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /audit"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 1, "results": []any{
			map[string]any{"id": 1, "recorded_at": "now", "action": "album.import", "target": "album:3", "operation_id": 7, "changes": map[string]any{}},
		}})
	}
	rec := get(t, h, "/audit?operation=7")
	mustContain(t, rec, `href="/imports/7">Import 7`, "Only changes made by", `name="operation" value="7"`)
	if r, _ := f.last(t, "GET", "/audit"); r.URL.Query().Get("operation_id") != "7" {
		t.Errorf("query %s", r.URL.RawQuery)
	}
}

func TestNoMetadataSource(t *testing.T) {
	cases := []struct {
		plugins any
		want    bool
	}{
		{[]any{"info", "web"}, true},
		{[]any{"musicbrainz", "web"}, false},
		{"web deezer", false},
		{"web info", true},
		{nil, false}, // unknown: don't warn
	}
	for _, c := range cases {
		cfg := map[string]any{}
		if c.plugins != nil {
			cfg["plugins"] = c.plugins
		}
		if got := noMetadataSource(cfg); got != c.want {
			t.Errorf("%v: got %v", c.plugins, got)
		}
	}
}

func TestImportWarnsWithoutMetadataSource(t *testing.T) {
	f, h := newTest(t)
	importPathsRoute(f)
	f.routes["GET /config"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"plugins": []string{"info", "web"}})
	}
	mustContain(t, get(t, h, "/import"), "enables no metadata source")
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": 7, "status": "waiting_for_choice", "request": map[string]any{"paths": []string{"/in/a"}},
			"result": map[string]any{"prompt": map[string]any{"id": "p1", "kind": "album", "paths": []string{"/in/a"},
				"choices": []string{"asis", "skip", "search", "abort"}, "current": map[string]any{"artist": "AC/DC", "album": "Back in Black"},
				"recommendation": "none", "candidates": []any{}}}})
	}
	rec := get(t, h, "/imports/7")
	mustContain(t, rec, "enables no metadata source")
	if strings.Contains(rec.Body.String(), "No Candidates found") {
		t.Error("blames the match, not the config")
	}
}

func TestSourceURL(t *testing.T) {
	album := candidate{DataSource: "MusicBrainz", ID: "abc"}
	track := candidate{DataSource: "MusicBrainz", ID: "def", Track: &fileChanges{}}
	if u := sourceURL(album); u != "https://musicbrainz.org/release/abc" {
		t.Error(u)
	}
	if u := sourceURL(track); u != "https://musicbrainz.org/recording/def" {
		t.Error(u)
	}
	if u := sourceURL(candidate{DataSource: "Discogs", ID: "1"}); u != "" {
		t.Error(u)
	}
}

func TestSingletonOutcomeLinksItemsOnly(t *testing.T) {
	f, h := newTest(t)
	op := finishedImport(nil)
	op["result"].(map[string]any)["outcomes"] = []any{map[string]any{"paths": []string{"/in/a"}, "skipped": false, "items": []any{map[string]any{"id": 11, "album_id": nil}}}}
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, op) }
	rec := get(t, h, "/imports/7")
	mustContain(t, rec, `href="/items/11"`)
	if strings.Contains(rec.Body.String(), `href="/albums/`) {
		t.Error("an Album link for a singleton")
	}
}
