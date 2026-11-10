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
	f.routes["GET /config"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"import": map[string]any{"copy": true, "move": false}})
	}
	mustContain(t, get(t, h, "/import"), `href="/import?path=%2Fin%2Fincoming"`, "Import Paths", ">Import<")
	rec := get(t, h, "/import?path=/in/incoming")
	mustContain(t, rec,
		`name="path" value="/in/incoming/Abbey Road"`, // one Import button per folder
		`name="path" value="/in/incoming">Import this folder`,
		`href="/import">Up`, "3.0 MB", "copied into the Library; the originals stay")
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
	if r.Header.Get("X-Client") != "beets-ui" {
		t.Error("no X-Client")
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

func TestImportListShowsTheActiveImport(t *testing.T) {
	f, h := newTest(t)
	importPathsRoute(f)
	f.routes["GET /imports"] = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("status") {
			writeJSON(w, 200, map[string]any{"total": 1, "results": []any{
				map[string]any{"id": 9, "status": "waiting_for_choice", "request": map[string]any{"paths": []string{"/in/b"}}},
			}})
			return
		}
		writeJSON(w, 200, map[string]any{"total": 45, "results": []any{
			map[string]any{"id": 8, "status": "completed", "request": map[string]any{"paths": []string{"/in/a"}}, "created_at": "2026-11-09T09:00:00Z",
				"counts": map[string]any{"choices": 3, "tasks": 3, "skipped": 1, "items": 24}},
			map[string]any{"id": 7, "status": "aborted", "request": map[string]any{"paths": []string{"/in/c"}}, "created_at": "2026-11-08T09:00:00Z",
				"counts": map[string]any{"choices": 1, "tasks": 0, "skipped": 0, "items": 0}},
		}})
	}
	rec := get(t, h, "/import?path=/in/incoming&offset=20")
	mustContain(t, rec, "Import 9 is waiting for choice", `href="/imports/9"`, `href="/imports/8"`, "2026-11-09 09:00",
		"24 Items imported · 1 of 3 Import Tasks skipped", "Nothing imported", "45 Imports",
		`href="/import?path=%2Fin%2Fincoming#imports">Newer`, `href="/import?offset=40&amp;path=%2Fin%2Fincoming#imports">Older`)
	if r, _ := f.last(t, "GET", "/imports"); r.URL.Query().Get("status") == "" {
		t.Error("the active Import isn't looked up by status")
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
				"tracks":      []any{map[string]any{"path": "/in/a/01.mp3", "changes": map[string]any{"title": map[string]any{"old": "come together", "new": "Come Together"}}}},
				"extra_items": []any{}, "extra_tracks": []any{"Her Majesty"},
			}},
		}}}
}

func TestAlbumPromptShowsCandidatesAndChoices(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, albumPrompt()) }
	rec := get(t, h, "/imports/7")
	mustContain(t, rec, "92%", "The Beatles – Abbey Road", "1969", "MusicBrainz", "<details", " open",
		"01.mp3", "<del>come together</del> → <ins>Come Together</ins>", "Her Majesty",
		`name="action" value="apply"`, `name="candidate" value="0"`, `name="prompt_id" value="p1"`,
		">Use as-is<", ">Import as tracks<", ">Group albums<", ">Skip<", "Search again", `value="Abbey Rd"`, "medium")
	if strings.Contains(rec.Body.String(), `hx-trigger`) {
		t.Error("polls while waiting for a Choice")
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
		">Skip<", ">Keep both<", ">Merge<", ">Remove old<", "deleting their files")
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
	mustContain(t, rec, `href="/albums/3"`, "2 Items", "Skipped", "Nothing imported", "Applied Candidate 1", `href="/audit?operation=7"`)
	if strings.Contains(rec.Body.String(), "Abort Import") {
		t.Error("finished Import offers Abort")
	}
}

func TestAuditFiltersByImport(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /audit"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 1, "results": []any{
			map[string]any{"id": 1, "at": "now", "action": "album.import", "target": "album:3", "operation_id": 7, "changes": map[string]any{}},
		}})
	}
	rec := get(t, h, "/audit?operation=7")
	mustContain(t, rec, `href="/imports/7">Import 7`, "Only changes made by", `name="operation" value="7"`)
	if r, _ := f.last(t, "GET", "/audit"); r.URL.Query().Get("operation_id") != "7" {
		t.Errorf("query %s", r.URL.RawQuery)
	}
}
