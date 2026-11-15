package main

import (
	"net/http"
	"strings"
	"testing"
)

func dupePrompt(kind string, current map[string]string, cands ...map[string]any) map[string]any {
	list := make([]any, len(cands))
	for i, c := range cands {
		c["index"], c["distance"], c["data_source"], c["tracks"] = i, 0.1, "MusicBrainz", []any{}
		list[i] = c
	}
	return map[string]any{"id": 7, "status": "waiting_for_choice", "request": map[string]any{"paths": []string{"/in"}}, "result": map[string]any{
		"choices": []any{}, "expected_tasks": 2, "tasks_done": 0,
		"prompt": map[string]any{"id": "p1", "kind": kind, "paths": []string{"/in/a"}, "choices": []string{"apply", "asis", "skip", "abort"},
			"current": current, "recommendation": "medium", "candidates": list}}}
}

func afro() map[string]any {
	return map[string]any{"artist": "Afro-Cuban All Stars", "album": "Distinto, diferente", "id": "mb-1"}
}

func libraryHas(f *fakeAPI, results ...map[string]any) {
	list := make([]any, len(results))
	for i, r := range results {
		list[i] = r
	}
	f.routes["GET /list"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": len(list), "limit": 100, "offset": 0, "results": list})
	}
}

func TestPromptWarnsThatTheAlbumIsAlreadyInTheLibrary(t *testing.T) {
	f, h := newTest(t)
	other := map[string]any{"artist": "Afro-Cuban All Stars", "album": "En vivo", "id": "mb-3"}
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, dupePrompt("album", map[string]string{"artist": "Afro-Cuban All Stars", "album": "Distinto, diferente"}, afro(), afro(), other))
	}
	libraryHas(f, map[string]any{"id": 1, "albumartist": "Afro-Cuban All Stars", "album": "Distinto, diferente"})
	rec := get(t, h, "/imports/7")
	// The files' own tags are a duplicate: said beside them.
	mustContain(t, rec, `class="badge text-bg-warning fw-normal"`, "Already in your Library:", `href="/albums/1"`, "Album 1")
	// The two Candidates that are the same Album say so in their verdict, the other doesn't.
	if n := strings.Count(rec.Body.String(), "Your Library already has this release as"); n != 2 {
		t.Errorf("%d Candidates marked, want 2", n)
	}
	mustContain(t, rec, "beets will ask whether to skip it, keep both, remove the old one or merge them")
	// One question to the Library, exact, one group per distinct name, and "scope" is Albums.
	r, _ := f.last(t, "GET", "/list")
	q := r.URL.Query()
	if q.Get("scope") != "album" || strings.Count(q.Get("query"), " , ") != 1 ||
		!strings.Contains(q.Get("query"), `'albumartist:=Afro-Cuban All Stars' 'album:=Distinto, diferente'`) ||
		!strings.Contains(q.Get("query"), `'album:=En vivo'`) {
		t.Errorf("asked %v", q)
	}
}

func TestPromptWithNothingInTheLibraryHasNoWarning(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, dupePrompt("album", map[string]string{"artist": "Afro-Cuban All Stars", "album": "Distinto, diferente"}, afro()))
	}
	libraryHas(f)
	rec := get(t, h, "/imports/7")
	if strings.Contains(rec.Body.String(), "Already in your Library") {
		t.Error("warns about a duplicate that isn't one")
	}
}

func TestOnlyAnExactNameCountsAsADuplicate(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, dupePrompt("album", map[string]string{"artist": "Afro-Cuban All Stars", "album": "Distinto, diferente"}, afro()))
	}
	// A near miss that came back anyway is not what beets would call a duplicate.
	libraryHas(f, map[string]any{"id": 2, "albumartist": "Afro-Cuban All Stars", "album": "Distinto, diferente (Deluxe)"})
	if strings.Contains(get(t, h, "/imports/7").Body.String(), "Already in your Library") {
		t.Error("a different name is a duplicate")
	}
}

func TestACandidateThatIsADuplicateIsMarkedEvenWhenTheFilesTagsAreNot(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, dupePrompt("album", map[string]string{"artist": "afro cuban", "album": "distinto"}, afro()))
	}
	libraryHas(f, map[string]any{"id": 1, "albumartist": "Afro-Cuban All Stars", "album": "Distinto, diferente"})
	rec := get(t, h, "/imports/7")
	mustContain(t, rec, "Your Library already has this release as", `href="/albums/1"`)
	if strings.Contains(rec.Body.String(), `class="badge text-bg-warning fw-normal"`) {
		t.Error("the files' own tags are not a duplicate, only the Candidate is")
	}
}

func TestItemPromptChecksItemsByArtistAndTitle(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, dupePrompt("item", map[string]string{"artist": "Radiohead", "title": "Airbag"},
			map[string]any{"artist": "Radiohead", "title": "Airbag", "id": "t1", "track": map[string]any{"path": "/in/a/1.mp3", "changes": map[string]any{}, "fields": map[string]any{}}}))
	}
	libraryHas(f, map[string]any{"id": 5, "artist": "Radiohead", "title": "Airbag"})
	rec := get(t, h, "/imports/7")
	mustContain(t, rec, "Already in your Library:", `href="/items/5"`, "Item 5")
	r, _ := f.last(t, "GET", "/list")
	if r.URL.Query().Get("scope") != "item" || !strings.Contains(r.URL.Query().Get("query"), `'artist:=Radiohead' 'title:=Airbag'`) {
		t.Errorf("asked %v", r.URL.Query())
	}
}

func TestDuplicatePromptsAndFailuresDoNotAskTheLibrary(t *testing.T) {
	f, h := newTest(t)
	// beets has already found the duplicate: nothing to look up.
	op := dupePrompt("duplicate", map[string]string{})
	op["result"].(map[string]any)["prompt"].(map[string]any)["existing"] = []any{map[string]any{"id": 4, "albumartist": "AC/DC", "album": "Back in Black"}}
	op["result"].(map[string]any)["prompt"].(map[string]any)["choices"] = []string{"skip", "keep", "remove", "abort"}
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, op) }
	rec := get(t, h, "/imports/7")
	mustContain(t, rec, "Already in the Library.")
	for _, r := range f.requests {
		if r.URL.Path == "/list" {
			t.Error("looked up a duplicate beets had already found")
		}
	}
	// If the Library can't answer, the prompt still shows.
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, dupePrompt("album", map[string]string{"artist": "A", "album": "B"}, afro()))
	}
	f.routes["GET /list"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 500, map[string]any{"detail": "boom"}) }
	mustContain(t, get(t, h, "/imports/7"), "Match this Album.")
}
