package main

import (
	"net/http"
	"testing"
)

func TestAuditListsChanges(t *testing.T) {
	f, h := newTest(t)
	f.routes["GET /audit"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"total": 2, "limit": 50, "offset": 0, "results": []any{
			map[string]any{"id": 2, "at": "2026-11-09T22:40:00Z", "client": "beets-ui", "action": "album.modify", "target": "album:5", "operation_id": 7,
				"changes": map[string]any{"year": map[string]any{"old": 1969, "new": 1970}}},
			map[string]any{"id": 1, "at": "2026-11-09T22:39:00Z", "client": nil, "action": "art.remove", "target": "album:5", "operation_id": nil,
				"changes": map[string]any{"artpath": map[string]any{"old": "/m/cover.jpg", "new": nil}, "archived": []any{3}}},
		}})
	}
	rec := get(t, h, "/audit?target=album:5&action=album.modify")
	mustContain(t, rec, `href="/modifications/7"`, `href="/albums/5"`, "<del>1969</del> → <ins>1970</ins>", "<del>/m/cover.jpg</del> → <ins></ins>", "[3]", "beets-ui", `<option value="album.modify" selected>`)
	r, _ := f.last(t, "GET", "/audit")
	if q := r.URL.Query(); q.Get("target") != "album:5" || q.Get("action") != "album.modify" {
		t.Errorf("query %v", q)
	}
}

func TestAuditWithoutEndpointSaysSo(t *testing.T) {
	_, h := newTest(t)
	mustContain(t, get(t, h, "/audit"), "has no <code>GET /audit</code>")
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
