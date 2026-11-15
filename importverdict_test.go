package main

import (
	"net/http"
	"strings"
	"testing"
)

// field is one field of a Candidate's first track, old and new.
func field(old, new any) map[string]any { return map[string]any{"old": old, "new": new} }

// whiteChristmas builds a Candidate shaped like the real ones of Import 3.
func whiteChristmas(index int, distance float64, year int, label string, unmatched, missing int, titles ...[2]string) map[string]any {
	var tracks []any
	for i, t := range titles {
		fields := map[string]any{"title": field(t[0], t[1]), "track": field(i+1, i+1), "length": field(120.0, 120.0)}
		if i == 0 {
			for k, v := range map[string]any{"albumartist": field("", "Bing Crosby"), "album": field("White Christmas", "White Christmas"),
				"year": field(1992, year), "month": field(0, 0), "day": field(0, 0), "label": field("", label),
				"catalognum": field("", "15 444"), "country": field("", "US"), "media": field("", "CD"), "albumtype": field("", "album")} {
				fields[k] = v
			}
		}
		tracks = append(tracks, map[string]any{"path": "/in/Bing/" + t[0] + ".mp3", "changes": map[string]any{}, "fields": fields})
	}
	extra := []any{}
	for i := 0; i < unmatched; i++ {
		extra = append(extra, "/in/Bing/extra "+string(rune('a'+i))+".mp3")
	}
	gone := []any{}
	for i := 0; i < missing; i++ {
		gone = append(gone, "Bonus "+string(rune('A'+i)))
	}
	return map[string]any{"index": index, "distance": distance, "artist": "Bing Crosby", "album": "White Christmas", "year": year, "id": "mb", "data_source": "MusicBrainz",
		"tracks": tracks, "extra_items": extra, "extra_tracks": gone}
}

func matchPrompt(rec string, cands ...map[string]any) map[string]any {
	list := make([]any, len(cands))
	for i, c := range cands {
		list[i] = c
	}
	return map[string]any{"id": 7, "status": "waiting_for_choice", "request": map[string]any{"paths": []string{"/in"}}, "result": map[string]any{
		"choices": []any{}, "expected_tasks": 50, "tasks_done": 0,
		"prompt": map[string]any{"id": "p1", "kind": "album", "paths": []string{"/in/Bing"}, "choices": []string{"apply", "asis", "skip", "tracks", "albums", "search", "abort"},
			"current": map[string]string{"artist": "Bing Crosby", "album": "White Christmas"}, "recommendation": rec, "candidates": list}}}
}

var same = [2]string{"White Christmas", "White Christmas"}
var renamed = [2]string{"Let It Snow! Let It Snow!", "Let It Snow, Let It Snow"}

func TestTheBestMatchIsSaidToBeSafeWhenItOnlyFillsAndRenames(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, matchPrompt("medium", whiteChristmas(0, 0.13, 1992, "LaserLight Digital", 0, 0, same, renamed)))
	}
	rec := get(t, h, "/imports/7")
	mustContain(t, rec, "Best match", "87%", "Bing Crosby – White Christmas", "1992 · LaserLight Digital · US · CD",
		"Renames 1 of 2 tracks", "Fills in 6 empty fields", "label: <ins>LaserLight Digital</ins>",
		"All 2 files match a track.", "No other field you have is overwritten.")
	for _, gone := range []string{"Changes ", "match no track", "your files don"} {
		if strings.Contains(rec.Body.String(), gone) {
			t.Errorf("a safe match says %q", gone)
		}
	}
}

func TestOverwritesAndLeftOutFilesComeFirst(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, matchPrompt("low", whiteChristmas(0, 0.59, 1998, "MCA", 3, 2, same, renamed)))
	}
	body := get(t, h, "/imports/7").Body.String()
	order := []string{"3 of your 5 files match no track", "extra a.mp3", "This release has 2 tracks your files", "Bonus A", "Changes release date",
		"<del>1992</del>", "<ins>1998</ins>", "Renames 1 of 2 tracks", "Fills in"}
	prev := -1
	for _, s := range order {
		i := strings.Index(body, s)
		if i < 0 || i < prev {
			t.Fatalf("%q missing or out of order", s)
		}
		prev = i
	}
	if strings.Contains(body, "overwritten.") || strings.Contains(body, "files match a track.") {
		t.Error("calls a match safe that overwrites and leaves files out")
	}
}

func TestEveryActionIsInOneRowWithAKey(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, matchPrompt("medium", whiteChristmas(0, 0.13, 1992, "L", 0, 0, same)))
	}
	rec := get(t, h, "/imports/7")
	mustContain(t, rec,
		// Apply submits the best Candidate's own form; the rest share one.
		`<form id="apply-0" method="post" action="/imports/7/choice" hidden>`, `name="candidate" value="0"`,
		`<button form="apply-0" class="btn btn-primary" name="action" value="apply" data-key="a">`,
		`form="choose" class="btn btn-outline-secondary" name="action" value="asis" data-key="u"`,
		`value="skip" data-key="s"`, `data-key="/"`, "More", `value="tracks"`, `value="albums"`,
		"<kbd>A</kbd> apply the best", "<kbd>U</kbd> use as-is", "<kbd>S</kbd> skip", "<kbd>/</kbd> search",
		`src="/public/static/importkeys.js?v=`)
}

func TestOtherMatchesAreAnOpenTable(t *testing.T) {
	f, h := newTest(t)
	op := matchPrompt("medium",
		whiteChristmas(0, 0.13, 1992, "LaserLight", 0, 0, same),
		whiteChristmas(1, 0.59, 1998, "MCA", 3, 0, same, renamed),
		whiteChristmas(2, 0.66, 1990, "World Star", 0, 2, same))
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, op) }
	rec := get(t, h, "/imports/7")
	body := rec.Body.String()
	// Only the best match has a key: a number key could apply a weak match by mistake.
	if strings.Count(body, `data-key="`) != 4 || strings.Contains(body, "apply another") {
		t.Errorf("keys other than A, U, S and /: %d", strings.Count(body, `data-key="`))
	}
	mustContain(t, rec, "2 other matches", "— 41% and below", "<th>Your files</th>", "<th>Overwrites</th>",
		"2 of 5", "release date 1992 → 1998", "+2 missing",
		`<button form="apply-1" class="btn btn-sm btn-outline-primary" name="action" value="apply" onclick="event.stopPropagation()">Apply</button>`,
		`data-bs-target="#match-1"`, `id="match-1"`, "Click a row for what applying it does.")
	// Open from the start, however sure beets is of the best.
	if !strings.Contains(body, `class="mb-3 other-matches" open`) {
		t.Error("the other matches are folded away")
	}
}

func TestNoMatchMakesSearchTheFirstAction(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, matchPrompt("none")) }
	rec := get(t, h, "/imports/7")
	mustContain(t, rec, "No release found for these files.", `class="btn btn-primary" data-bs-toggle="collapse" data-bs-target="#import-search" data-key="/"`,
		`class="collapse show" id="import-search"`, `value="asis" data-key="u"`, `value="skip" data-key="s"`)
	if strings.Contains(rec.Body.String(), "apply the best") || strings.Contains(rec.Body.String(), "Best match") {
		t.Error("talks about a match there isn't")
	}
}

func TestVerdictOf(t *testing.T) {
	c := candidate{Tracks: []fileChanges{
		{Path: "/a/1.mp3", Fields: map[string]change{"title": {"one", "One"}, "track": {"1", "2"}, "label": {"", "X"}, "country": {"GB", "US"},
			"albumartist": {"A", "A"}, "album": {"B", "B"}, "year": {"1", "1"}, "month": {"0", "0"}, "day": {"0", "0"}}},
		{Path: "/a/2.mp3", Fields: map[string]change{"title": {"", "Two"}, "track": {"2", "2"}}},
	}, ExtraItems: []string{"/a/3.mp3"}, ExtraTracks: []any{"Hidden"}}
	v := verdictOf(importPrompt{Kind: "album"}, c)
	if v.Files != 3 || len(v.Unmatched) != 1 || v.Unmatched[0] != "3.mp3" || len(v.Missing) != 1 || v.Tracks() != 2 {
		t.Errorf("files %+v", v)
	}
	// An empty title being given one is not a rename; a renumbering is counted apart.
	if len(v.Renames) != 1 || v.Renumbers != 1 {
		t.Errorf("renames %d, renumbers %d", len(v.Renames), v.Renumbers)
	}
	if len(v.Fills) != 1 || v.Fills[0].Label != "Label" || len(v.Overwrites) != 1 || v.Overwrites[0].Label != "Country" {
		t.Errorf("fills %+v overwrites %+v", v.Fills, v.Overwrites)
	}
	if v.AllMatch() || v.Keeps() != "" || v.OverwritesText() != "country GB → US" {
		t.Errorf("%v %q %q", v.AllMatch(), v.Keeps(), v.OverwritesText())
	}
	if (verdict{Album: true, Files: 2}).Keeps() != "Nothing you have is overwritten." || (verdict{Album: true, Files: 0}).AllMatch() {
		t.Error("an untouched match")
	}
}

func TestKeysScriptIsServed(t *testing.T) {
	_, h := newTest(t)
	rec := get(t, h, assetURL("importkeys.js"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "data-key") || !strings.Contains(rec.Body.String(), "getClientRects") {
		t.Errorf("got %d", rec.Code)
	}
}

func TestTheBestMatchShowsItsRenamesAndFillsOpenAndTheOthersFolded(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, matchPrompt("medium",
			whiteChristmas(0, 0.13, 1992, "LaserLight", 0, 0, same, renamed),
			whiteChristmas(1, 0.59, 1998, "MCA", 0, 0, same, renamed)))
	}
	body := get(t, h, "/imports/7").Body.String()
	if n := strings.Count(body, `<details class="d-inline" open><summary>Renames`); n != 1 {
		t.Errorf("%d open Renames, want the best match's only", n)
	}
	if n := strings.Count(body, `<details class="d-inline" open><summary>Fills in`); n != 1 {
		t.Errorf("%d open Fills in, want the best match's only", n)
	}
	if !strings.Contains(body, `<details class="d-inline"><summary>Renames`) {
		t.Error("an other match's renames are open")
	}
}
