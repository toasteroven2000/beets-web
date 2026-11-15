package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAuditListsChanges(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /audit"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 2, "limit": 50, "offset": 0, "results": []any{
			map[string]any{"id": 2, "recorded_at": "2026-11-09T22:40:00Z", "client": "beets-ui", "action": "album.modify", "target": "album:5", "operation_id": 7,
				"changes": map[string]any{"year": map[string]any{"old": 1969, "new": 1970}}},
			map[string]any{"id": 1, "recorded_at": "2026-11-09T22:39:00Z", "client": nil, "action": "art.remove", "target": "album:5", "operation_id": nil,
				"changes": map[string]any{"artpath": map[string]any{"old": "/m/cover.jpg", "new": nil}, "trashed_to": []any{map[string]any{"from": "/m/cover.jpg", "to": "/trash/art/cover.jpg"}}}},
		}})
	}
	rec := get(t, h, "/audit?target=album:5&action=album.modify")
	// Each change reads as one line, and the entry itself is a click away.
	mustContain(t, rec, `href="/modifications/7"`, `href="/albums/5"`, "year 1969 → 1970", "removed cover.jpg, moved to the Trash",
		`<a href="/audit/2">2</a>`, "2026-11-09T22:40:00.000+00:00", `<option value="album.modify" selected>`)
	r, _ := f.last(t, "GET", "/audit")
	if q := r.URL.Query(); q.Get("target") != "album:5" || q.Get("action") != "album.modify" {
		t.Errorf("query %v", q)
	}
}

func TestRecordPageLinksToItsAudit(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /items/5"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": 5, "title": "Let Down", "artist": "Radiohead"})
	}
	mustContain(t, get(t, h, "/items/5"), `href="/audit?target=item%3A5"`)
}

func TestModificationIDsMayBeNumbers(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /modifications"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 1, "results": []any{
			map[string]any{"id": 12, "status": "applied", "request": map[string]any{"scope": "item", "query": "id:1", "set": map[string]any{"year": "1970"}}, "created_at": "now"},
		}})
	}
	mustContain(t, get(t, h, "/modifications"), `href="/modifications/12"`, ">Modifications<")
}

func TestAuditValueIsNotTruncated(t *testing.T) {
	long := strings.Repeat("x", 1000)
	got := auditValue(map[string]any{"a": long})
	if !strings.Contains(got, long) || !strings.Contains(got, "\n") || strings.Contains(got, "…") {
		t.Errorf("auditValue cut a long value: %d chars", len(got))
	}
}

func TestMsTimeShowsThreeDecimals(t *testing.T) {
	for in, want := range map[string]string{
		"2026-11-14T09:00:24.893931+00:00": "2026-11-14T09:00:24.893+00:00",
		"2026-11-14T09:00:24+00:00":        "2026-11-14T09:00:24.000+00:00",
		"2026-11-14T09:00:24.5Z":           "2026-11-14T09:00:24.500+00:00",
		"now":                              "now", // not a timestamp: left as it is
		"":                                 "",
	} {
		if got := msTime(in); got != want {
			t.Errorf("msTime(%q) = %q, want %q", in, got, want)
		}
	}
}

func auditEntryRoute(f *fakeAPI) {
	entry := map[string]any{"id": 12, "recorded_at": "2026-11-14T09:00:24.893931+00:00", "client": "beets-ui", "actor": "alex",
		"action": "album.modify", "target": "album:5", "operation_id": 7,
		"changes": map[string]any{"year": map[string]any{"old": 1969, "new": 1970}, "tags": []any{"a", "b"}}}
	f.routes["GET /audit/12"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, entry) }
	// The other entries for album:5, newest first.
	f.routes["GET /audit"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 22, "results": []any{entry,
			map[string]any{"id": 9, "recorded_at": "2026-11-13T17:38:16.454970+00:00", "client": nil, "actor": nil, "action": "art.upload", "target": "album:5", "operation_id": nil, "changes": map[string]any{}}}})
	}
}

func TestAuditListHasAnIDColumnLinkingToTheEntry(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /audit"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 1, "results": []any{
			map[string]any{"id": 12, "recorded_at": "2026-11-14T09:00:24.893931+00:00", "action": "art.remove", "target": "album:5", "operation_id": nil, "changes": map[string]any{}},
		}})
	}
	mustContain(t, get(t, h, "/audit"), "<th>ID</th>", `<a href="/audit/12">12</a>`, "2026-11-14T09:00:24.893+00:00")
}

func TestAuditEntryPageShowsItWhole(t *testing.T) {
	f, h := newTest(t)
	auditEntryRoute(f)
	rec := get(t, h, "/audit/12")
	mustContain(t, rec, "Audit entry 12", "Entry 12", "2026-11-14T09:00:24.893+00:00",
		"Year 1969 → 1970", // the summary is the heading
		`actor-mark">A</span>alex`, "via beets-ui", `href="/modifications/7"`, "Modification 7",
		`href="/audit?target=album%3A5"`, `href="/audit?actor=alex"`, `href="/audit?operation=7"`,
		`<span class="same">19</span><del>69</del><span class="same"></span>`, "<ins>70</ins>", // only what differs is marked
		`<div class="font-monospace small text-break">a</div>`,
		"The entry as beets-api sent it", "&#34;recorded_at&#34;: &#34;2026-11-14T09:00:24.893931+00:00&#34;")
	// The history of what it changed, with this entry marked and the rest a click away.
	mustContain(t, rec, "History of", `class="timeline-item current" href="/audit/12"`, `class="timeline-item" href="/audit/9"`,
		"2026-11-13T17:38:16.454+00:00", "20 more")
	if r, _ := f.last(t, "GET", "/audit"); r.URL.Query().Get("target") != "album:5" {
		t.Errorf("history query %v", r.URL.Query())
	}
}

func TestAuditSummaries(t *testing.T) {
	for _, c := range []struct {
		action  string
		changes map[string]any
		want    string
	}{
		{"item.modify", map[string]any{"title": map[string]any{"old": "a", "new": "b"}, "path": map[string]any{"old": "/x/a.mp3", "new": "/x/b.mp3"}}, "title a → b"},
		{"item.modify", map[string]any{"path": map[string]any{"old": "/x/a.mp3", "new": "/y/a.mp3"}}, "moved the file"},
		{"album.modify", map[string]any{"year": map[string]any{"old": 1, "new": 2}, "label": map[string]any{"old": "A", "new": "B"}, "genre": map[string]any{"old": "x", "new": "y"}}, "genre x → y; label A → B (+1 more)"},
		{"album.import", map[string]any{"source": []any{"/in/Back in Black"}, "match": map[string]any{"data_source": "MusicBrainz", "id": "x"}, "items": []any{map[string]any{"id": 1, "path": "/a"}, map[string]any{"id": 2, "path": "/b"}}}, "2 files from Back in Black/, matched on MusicBrainz"},
		{"album.import", map[string]any{"source": []any{"/in/x"}, "match": "asis", "items": []any{map[string]any{"id": 1, "path": "/a"}}}, "1 file from x/, imported as-is"},
		{"item.import", map[string]any{"source": []any{"/in/x"}, "match": "asis", "path": "/a"}, "1 file from x/, imported as-is"},
		{"album.remove", map[string]any{"removed": map[string]any{"album": "Let It Be"}, "replaced_by": "album:6"}, "removed “Let It Be” (replaced by album:6)"},
		{"album.remove", map[string]any{"removed": map[string]any{"album": map[string]any{"album": "Let It Be"}, "items": []any{}}, "files": []any{}, "undo_of_import": json.Number("9")}, "undid Import 9: removed “Let It Be”"},
		{"item.remove", map[string]any{"removed": map[string]any{"title": "Airbag"}, "replaced_by": "item:2"}, "removed “Airbag” (replaced by item:2)"},
		{"art.upload", map[string]any{"artpath": map[string]any{"old": nil, "new": "/m/cover.jpg"}, "bytes": json.Number("48152"), "format": "jpg"}, "set cover.jpg, 47 KB jpg"},
		{"art.remove", map[string]any{"artpath": map[string]any{"old": "/m/cover.jpg", "new": nil}, "trashed_to": []any{}}, "removed cover.jpg"},
		{"trash.purge", map[string]any{"files": json.Number("12"), "bytes": json.Number("3250585")}, "purged 12 files · 3.1 MB"},
		{"album.remove", map[string]any{"removed": map[string]any{"album": "X"}, "trashed_to": []any{map[string]any{"from": "/a", "to": "/t/a"}}, "left_in_place": []any{}}, "removed “X”, moved 1 file to the Trash"},
	} {
		if got := auditSummary(auditEntry{Action: c.action, Changes: c.changes}); got != c.want {
			t.Errorf("%s: %q, want %q", c.action, got, c.want)
		}
	}
}

func TestTextDiffKeepsWhatIsShared(t *testing.T) {
	a, b := textDiff("/m/Radiohead/01 Airbag.mp3", "/m/Radiohead/01 Airbag foobar.mp3")
	if a != (diffText{"/m/Radiohead/01 Airbag", "", ".mp3"}) || b != (diffText{"/m/Radiohead/01 Airbag", " foobar", ".mp3"}) {
		t.Errorf("%+v %+v", a, b)
	}
	if a, b := textDiff("é1", "é2"); a.Pre != "é" || a.Mid != "1" || b.Mid != "2" {
		t.Errorf("multibyte: %+v %+v", a, b)
	}
}

func TestAChangeThatOnlyAddsTextHasNoSpaceInsideTheValue(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /audit/12"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": 12, "recorded_at": "2026-11-14T09:00:24Z", "action": "item.modify", "target": "item:1",
			"changes": map[string]any{"title": map[string]any{"old": "Airbag", "new": "Airbag 2"}}})
	}
	f.routes["GET /audit"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 0, "results": []any{}})
	}
	// The shared text and the marked text are adjacent, so no space appears between
	// them; the empty <del> is hidden by CSS (.audit-table del:empty).
	mustContain(t, get(t, h, "/audit/12"), `<span class="same">Airbag</span><del></del><span class="same"></span>`, `<span class="same">Airbag</span><ins> 2</ins><span class="same"></span>`)
}

func TestNavHasNoArtistsLink(t *testing.T) {
	_, h := newTest(t)
	rec := get(t, h, "/library")
	if strings.Contains(rec.Body.String(), `href="/artists"`) {
		t.Error("the nav still links to Artists")
	}
	mustContain(t, rec, `href="/library"`, `href="/import"`)
}

func TestAuditEntryThatDoesNotExistIs404(t *testing.T) {
	_, h := newTest(t)
	for _, path := range []string{"/audit/99", "/audit/abc"} {
		if rec := get(t, h, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
}

func TestModificationsShowTimesToTheMillisecond(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /modifications"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 1, "results": []any{
			map[string]any{"id": 3, "status": "applied", "request": map[string]any{"scope": "item", "query": "id:1", "set": map[string]any{}}, "created_at": "2026-11-14T09:00:00.466342+00:00"},
		}})
	}
	rec := get(t, h, "/modifications")
	mustContain(t, rec, "2026-11-14T09:00:00.466+00:00")
	if strings.Contains(rec.Body.String(), "466342") {
		t.Error("shows microseconds")
	}
}
