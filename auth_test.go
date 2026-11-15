package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const anon = "X-Test-Anonymous"

func TestRequestsWithoutTheActorHeaderAreRefusedAndTouchNothing(t *testing.T) {
	f, h := newTest(t)
	for _, path := range []string{"/", "/library", "/audit", "/profile", "/imports/7", "/public/static/app.css"} {
		rec := get(t, h, path, anon, "1")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s: %d", path, rec.Code)
		}
		mustContain(t, rec, "the Remote-User header is missing")
		if strings.Contains(rec.Body.String(), "Authelia") || strings.Contains(rec.Body.String(), "forward auth") {
			t.Error("the 401 explains more than that the header is missing")
		}
	}
	if rec := post(t, h, "/modifications/7/confirm", nil, anon, "1"); rec.Code != http.StatusUnauthorized {
		t.Errorf("POST: %d", rec.Code)
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused request reached beets-api: %d calls", len(f.requests))
	}
}

func TestPingNeedsNoHeaderAndNeverCallsBetsApi(t *testing.T) {
	f, h := newTest(t)
	rec := get(t, h, "/ping", anon, "1")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "ok" {
		t.Errorf("GET /ping: %d %q", rec.Code, rec.Body.String())
	}
	head := httptest.NewRecorder()
	h.ServeHTTP(head, httptest.NewRequest("HEAD", "/ping", nil))
	if head.Code != http.StatusOK {
		t.Errorf("HEAD /ping: %d", head.Code)
	}
	if len(f.requests) != 0 {
		t.Error("ping called beets-api")
	}
	if rec := post(t, h, "/ping", nil, anon, "1"); rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /ping: %d", rec.Code)
	}
}

func TestActorAndClientAreSentToBeetsAPI(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /albums/2"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"id": 2}) }
	get(t, h, "/albums/2", "Remote-User", "alex", "Remote-Name", "Alex Rivera")
	r, _ := f.last(t, "GET", "/albums/2")
	if r.Header.Get("X-Beets-API-Actor") != "alex" || r.Header.Get("X-Beets-API-Client") != "beets-web" || r.Header.Get("X-Client") != "" {
		t.Errorf("headers %v", r.Header)
	}
}

func TestWritesCarryTheActor(t *testing.T) {
	f, h := newTest(t)
	f.routes["POST /modifications/7/confirm"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 202, map[string]any{}) }
	post(t, h, "/modifications/7/confirm", nil, "Remote-User", "bob")
	if r, _ := f.last(t, "POST", "/modifications/7/confirm"); r.Header.Get("X-Beets-API-Actor") != "bob" {
		t.Errorf("actor %q", r.Header.Get("X-Beets-API-Actor"))
	}
}

func TestTheActorHeaderCanBeAnotherOne(t *testing.T) {
	f := &fakeAPI{routes: map[string]func(http.ResponseWriter, *http.Request){}}
	api := httptest.NewServer(f)
	t.Cleanup(api.Close)
	s := newServer(api.URL, api.Client())
	s.actorHeader = "Remote-Email"
	h := s.routes()
	if rec := get(t, h, "/profile", "Remote-User", "alex"); rec.Code != http.StatusUnauthorized {
		t.Errorf("a different header counts: %d", rec.Code)
	} else {
		mustContain(t, rec, "the Remote-Email header is missing")
	}
	rec := get(t, h, "/profile", "Remote-User", "alex", "Remote-Email", "alex@example.com")
	mustContain(t, rec, "<strong>alex@example.com</strong>", "<code>Remote-Email</code>")
	get(t, h, "/library", "Remote-Email", "alex@example.com")
	if r, _ := f.last(t, "GET", "/list"); r.Header.Get("X-Beets-API-Actor") != "alex@example.com" {
		t.Errorf("actor %q", r.Header.Get("X-Beets-API-Actor"))
	}
}

func TestOverlongActorIsRefused(t *testing.T) {
	_, h := newTest(t)
	if rec := get(t, h, "/", "Remote-User", strings.Repeat("x", 201)); rec.Code != http.StatusBadRequest {
		t.Errorf("got %d", rec.Code)
	}
}

func TestNavbarHasThePersonsMenuWithProfileModificationsAndAudit(t *testing.T) {
	_, h := newTest(t)
	rec := get(t, h, "/library", "Remote-User", "alex", "Remote-Name", "Alex Rivera")
	mustContain(t, rec, "Alex Rivera",
		`<a class="dropdown-item" href="/profile">`, `<a class="dropdown-item" href="/modifications">`, `<a class="dropdown-item" href="/audit">`)
	for _, gone := range []string{`<a class="nav-link" href="/audit"`, `<a class="nav-link" href="/modifications"`, "href=\"/audit\" class=\"nav-link"} {
		if strings.Contains(rec.Body.String(), gone) {
			t.Errorf("still in the main nav: %s", gone)
		}
	}
	// Without a full name, the username.
	mustContain(t, get(t, h, "/library", "Remote-User", "alex"), "</i> alex</a>")
}

func TestMenuIsHighlightedOnItsPages(t *testing.T) {
	f, h := newTest(t)
	empty := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 0, "results": []any{}})
	}
	f.routes["GET /audit"], f.routes["GET /modifications"] = empty, empty
	for path, want := range map[string]string{
		"/audit": `<a class="dropdown-item active" aria-current="page" href="/audit">`, "/profile": `class="dropdown-item active" aria-current="page" href="/profile"`,
		"/modifications": `class="dropdown-item active" aria-current="page" href="/modifications"`,
	} {
		rec := get(t, h, path)
		mustContain(t, rec, want, `class="nav-link dropdown-toggle active"`)
	}
	if strings.Contains(get(t, h, "/library").Body.String(), "dropdown-toggle active") {
		t.Error("menu highlighted on Albums")
	}
}

func TestErrorPagesStillShowWhoIsSignedIn(t *testing.T) {
	_, h := newTest(t)
	rec := get(t, h, "/albums/9", "Remote-User", "alex", "Remote-Name", "Alex Rivera")
	mustContain(t, rec, "Something went wrong", "Alex Rivera")
}

func TestProfileShowsTheIdentityHeadersAndTheRecordedActor(t *testing.T) {
	_, h := newTest(t)
	rec := get(t, h, "/profile", "Remote-User", "alex", "Remote-Name", "Alex Rivera", "Remote-Email", "alex@example.com", "Remote-Groups", "admins, family")
	mustContain(t, rec, "Remote-User", "Remote-Name", "Remote-Email", "Remote-Groups", "alex@example.com", "Alex Rivera",
		`<span class="badge text-bg-secondary me-1">admins</span>`, `<span class="badge text-bg-secondary me-1">family</span>`,
		`<span class="badge text-bg-primary">Actor</span>`, "<strong>alex</strong>", "<code>Remote-User</code>")
	if n := strings.Count(rec.Body.String(), `text-bg-primary">Actor`); n != 1 {
		t.Errorf("Actor badge on %d rows", n)
	}
	// A header the proxy didn't send says so.
	mustContain(t, get(t, h, "/profile", "Remote-User", "alex"), "not sent")
	// Nothing else about the request is shown.
	if strings.Contains(get(t, h, "/profile", "Remote-User", "alex", "Cookie", "session=secret", "Authorization", "Bearer secret").Body.String(), "secret") {
		t.Error("the Profile page shows headers other than the identity ones")
	}
}

func TestConfigPageListsBeetsConfigFromTheMenu(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /config"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"import":    map[string]any{"copy": true, "languages": []any{"en", "fr"}},
			"plugins":   []any{"fetchart", "lyrics"},
			"acoustid":  map[string]any{"apikey": "REDACTED"},
			"directory": "/music",
		})
	}
	rec := get(t, h, "/config", "Remote-User", "alex")
	mustContain(t, rec, "import.copy", "true", "import.languages", "en; fr", "plugins", "fetchart; lyrics",
		"acoustid.apikey", "REDACTED", "directory", "/music",
		`class="dropdown-item active" aria-current="page" href="/config"`, `class="nav-link dropdown-toggle active"`)
	mustContain(t, get(t, h, "/profile", "Remote-User", "alex"), `href="/config"`)
}

func TestAuditShowsActorsAndFiltersByThem(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /audit"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 1, "limit": 50, "offset": 0, "results": []any{
			map[string]any{"id": 1, "recorded_at": "2026-11-10T09:00:00Z", "client": "beets-web", "actor": "alice", "action": "art.remove", "target": "album:5", "operation_id": nil, "changes": map[string]any{}},
		}})
	}
	rec := get(t, h, "/audit?actor=alice")
	mustContain(t, rec, "<th>Actor</th>", `actor-mark">A</span>alice`, `name="actor" value="alice"`, `href="/audit">Clear`)
	if r, _ := f.last(t, "GET", "/audit"); r.URL.Query().Get("actor") != "alice" {
		t.Errorf("query %v", r.URL.Query())
	}
}

func TestImportShowsWhoStartedItAndGaveEachChoice(t *testing.T) {
	f, h := newTest(t)
	op := finishedImport(map[string]any{"actor": "alice", "choices": []any{
		map[string]any{"paths": []string{"/in/a"}, "kind": "album", "action": "search", "actor": "bob"},
		map[string]any{"paths": []string{"/in/a"}, "kind": "album", "action": "apply", "candidate": 0, "actor": "carol"},
	}})
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, op) }
	// One Task, found by a search (bob) and then applied (carol): the Log has one row, by the last.
	mustContain(t, get(t, h, "/imports/7"), "Started by alice")
	rec := get(t, h, "/imports/7?tab=log")
	mustContain(t, rec, "Applied", "carol")
	if strings.Contains(rec.Body.String(), "bob") || strings.Count(rec.Body.String(), "<td class=\"small\">Applied</td>") != 1 {
		t.Error("a searched Task is listed more than once")
	}
}
