package main

// The server and its routes. There is no auth: it is for one person on one
// machine, and listens on 127.0.0.1 unless told otherwise.

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const pageSize = 100

type server struct {
	api *beetsAPI
}

func newServer(apiURL string, client *http.Client) *server {
	return &server{api: &beetsAPI{base: strings.TrimRight(apiURL, "/"), http: client}}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /public/static/", staticHandler())
	mux.HandleFunc("GET /{$}", s.handleHome)
	mux.HandleFunc("GET /artists", s.handleArtists)
	mux.HandleFunc("GET /artists/{name}", s.handleArtist)
	mux.HandleFunc("GET /albums", s.handleList(albums))
	mux.HandleFunc("GET /items", s.handleList(items))
	mux.HandleFunc("GET /search", s.handleSearch)
	mux.HandleFunc("GET /albums/{id}", s.handleRecord(albums))
	mux.HandleFunc("GET /items/{id}", s.handleRecord(items))
	mux.HandleFunc("GET /albums/{id}/art", s.handleArt)
	mux.HandleFunc("GET /modify", s.handleModifyForm)
	mux.HandleFunc("POST /modifications", s.handleCreateModification)
	mux.HandleFunc("GET /modifications", s.handleHistory)
	mux.HandleFunc("GET /modifications/{id}", s.handleModification)
	mux.HandleFunc("POST /modifications/{id}/confirm", s.handleConfirm)
	mux.HandleFunc("POST /modifications/{id}/again", s.handleAgain)
	return mux
}

// fail shows an error page; a 404 from beets-api stays a 404.
func fail(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	var e *apiError
	if errors.As(err, &e) && e.Status < 500 {
		status = e.Status
	}
	log.Printf("error: %v", err)
	render(w, status, errorPage(status, errorMessage(err)))
}

func errorMessage(err error) string {
	var e *apiError
	if errors.As(err, &e) {
		return e.Detail
	}
	return err.Error()
}

// separators are the Artist separators from beets-api's config, so the UI
// splits and queries Artists the same way GET /artists counts them.
func (s *server) separators(ctx context.Context) []string {
	cfg, err := s.api.Config(ctx)
	if err != nil {
		return defaultSeparators
	}
	section, _ := cfg["beets_api"].(map[string]any)
	list, ok := section["artist_separators"].([]any)
	if !ok {
		return defaultSeparators
	}
	var out []string
	for _, v := range list {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func offsetParam(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("offset"))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// isMore is an htmx request for the next batch of rows in a list.
func isMore(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" && offsetParam(r) > 0
}

// nextURL is this page's URL for the batch after res, or "" at the end.
func nextURL(r *http.Request, res listResult) string {
	next := res.Offset + len(res.Results)
	if next >= res.Total || len(res.Results) == 0 {
		return ""
	}
	q := r.URL.Query()
	q.Set("offset", strconv.Itoa(next))
	return r.URL.Path + "?" + q.Encode()
}

var listFields = map[scope][]string{
	albums: {"id", "albumartist", "album", "year", "albumtype", "added"},
	items:  {"id", "artist", "title", "album", "album_id", "track", "disc", "length", "year", "added"},
}

func (s *server) table(r *http.Request, k scope, query string, recent bool) (tableData, error) {
	ctx := r.Context()
	res, err := s.api.List(ctx, k, withSort(query, k, recent), pageSize, offsetParam(r), listFields[k])
	if err != nil {
		return tableData{}, err
	}
	return tableData{
		Scope:      k,
		Query:      query,
		Rows:       res.Results,
		Total:      res.Total,
		Next:       nextURL(r, res),
		Separators: s.separators(ctx),
	}, nil
}

func (s *server) handleArtists(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	res, err := s.api.Artists(r.Context(), q)
	p := artistsPage{Query: q, Artists: res.Results, Total: res.Total}
	if isStatus(err, http.StatusNotFound) {
		p.Unavailable = true
	} else if err != nil {
		fail(w, err)
		return
	}
	render(w, http.StatusOK, artistsView(p))
}

func (s *server) handleArtist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")
	k, ok := parseScope(r.URL.Query().Get("tab"))
	seps := s.separators(ctx)
	p := artistPage{Name: name, Queries: map[scope]string{}, Counts: map[scope]int{}}
	for _, each := range []scope{albums, items} {
		p.Queries[each] = artistQuery(each, name, seps)
		res, err := s.api.List(ctx, each, p.Queries[each], 1, 0, []string{"id"})
		if err != nil {
			fail(w, err)
			return
		}
		p.Counts[each] = res.Total
	}
	if !ok {
		k = albums
		if p.Counts[albums] == 0 && p.Counts[items] > 0 {
			k = items
		}
	}
	p.Tab = k
	t, err := s.table(r, k, p.Queries[k], false)
	if err != nil {
		fail(w, err)
		return
	}
	if isMore(r) {
		render(w, http.StatusOK, tableRows(t))
		return
	}
	if p.Counts[albums]+p.Counts[items] == 0 {
		render(w, http.StatusNotFound, errorPage(http.StatusNotFound, "No Album or Item credits "+name+"."))
		return
	}
	p.Table = t
	render(w, http.StatusOK, artistView(p))
}

func filtersFrom(r *http.Request) filters {
	q := r.URL.Query()
	get := func(k string) string { return strings.TrimSpace(q.Get(k)) }
	return filters{
		Query:     get("q"),
		YearFrom:  get("year_from"),
		YearTo:    get("year_to"),
		Genre:     get("genre"),
		AlbumType: get("albumtype"),
		Format:    get("format"),
		Recent:    q.Get("order") == "recent",
	}
}

func (s *server) handleList(k scope) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f := filtersFrom(r)
		t, err := s.table(r, k, f.Combined(), f.Recent)
		if err != nil {
			if isStatus(err, http.StatusBadRequest) && !isMore(r) {
				render(w, http.StatusBadRequest, listView(listPage{Scope: k, Filters: f, Error: errorMessage(err)}))
				return
			}
			fail(w, err)
			return
		}
		if isMore(r) {
			render(w, http.StatusOK, tableRows(t))
			return
		}
		render(w, http.StatusOK, listView(listPage{Scope: k, Filters: f, Table: t}))
	}
}

func (s *server) handleHome(w http.ResponseWriter, r *http.Request) {
	render(w, http.StatusOK, homeView())
}

func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := searchPage{Query: strings.TrimSpace(r.URL.Query().Get("q")), Tables: map[scope]tableData{}}
	if p.Query != "" {
		seps := s.separators(ctx)
		for _, k := range []scope{albums, items} {
			res, err := s.api.List(ctx, k, withSort(p.Query, k, false), 10, 0, listFields[k])
			if isStatus(err, http.StatusBadRequest) {
				p.Error = errorMessage(err)
				break
			}
			if err != nil {
				fail(w, err)
				return
			}
			p.Tables[k] = tableData{Scope: k, Query: p.Query, Rows: res.Results, Total: res.Total, Separators: seps}
		}
	}
	render(w, http.StatusOK, searchView(p))
}

func idParam(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		render(w, http.StatusNotFound, errorPage(http.StatusNotFound, "Not found."))
		return 0, false
	}
	return id, true
}

func (s *server) handleRecord(k scope) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		rec, err := s.api.Get(ctx, k, id)
		if err != nil {
			fail(w, err)
			return
		}
		p := recordPage{Scope: k, Tab: "items", Record: rec, Separators: s.separators(ctx)}
		if k == items || r.URL.Query().Get("tab") == "fields" {
			p.Tab = "fields"
		}
		if k == albums {
			res, err := s.api.List(ctx, items, "album_id:"+strconv.Itoa(id)+" disc+ track+", 1000, 0, listFields[items])
			if err != nil {
				fail(w, err)
				return
			}
			p.Items = tableData{Scope: items, Query: "album_id:" + strconv.Itoa(id), Rows: res.Results, Total: res.Total, Separators: p.Separators}
		} else if albumID := rec.Int("album_id"); albumID != 0 {
			if album, err := s.api.Get(ctx, albums, albumID); err == nil {
				p.Album = album
			}
		}
		render(w, http.StatusOK, recordView(p))
	}
}

func (s *server) handleArt(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	resp, err := s.api.AlbumArt(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Type", "Content-Length", "ETag", "Last-Modified", "Cache-Control"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	io.Copy(w, resp.Body)
}

// handleModifyForm is the bulk Modify form, for ticked rows (`id`) or for
// everything a query matches (`q`).
func (s *server) handleModifyForm(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	k, ok := parseScope(q.Get("scope"))
	if !ok {
		render(w, http.StatusBadRequest, errorPage(http.StatusBadRequest, "Choose Albums or Items to Modify."))
		return
	}
	query := strings.TrimSpace(q.Get("q"))
	if ids := q["id"]; len(ids) > 0 {
		var nums []int
		for _, v := range ids {
			if n, err := strconv.Atoi(v); err == nil {
				nums = append(nums, n)
			}
		}
		query = idsQuery(nums)
	}
	if query == "" {
		render(w, http.StatusBadRequest, errorPage(http.StatusBadRequest, "Nothing selected. Tick some rows, or use “Modify all”."))
		return
	}
	res, err := s.api.List(r.Context(), k, query, 1, 0, []string{"id"})
	if err != nil {
		fail(w, err)
		return
	}
	render(w, http.StatusOK, modifyView(modifyPage{Scope: k, Query: query, Count: res.Total, Back: q.Get("back")}))
}

// modifyRequest reads a Modify form: the record form's changed fields, plus
// any field/value rows and fields to delete.
func modifyRequest(form url.Values) (modRequest, error) {
	k, ok := parseScope(form.Get("scope"))
	if !ok {
		return modRequest{}, errors.New("choose Albums or Items to Modify")
	}
	req := modRequest{Scope: k.API(), Query: strings.TrimSpace(form.Get("query")), Set: map[string]string{}}
	if req.Query == "" {
		return modRequest{}, errors.New("a Modify needs a query")
	}
	deleted := map[string]bool{}
	del := func(field string) {
		if field != "" && !deleted[field] {
			deleted[field] = true
			req.Delete = append(req.Delete, field)
		}
	}
	for key, vals := range form {
		if field, ok := strings.CutPrefix(key, "f."); ok {
			if vals[0] != form.Get("o."+field) {
				req.Set[field] = vals[0]
			}
		}
	}
	keys, vals := form["set_field"], form["set_value"]
	for i, field := range keys {
		field = strings.TrimSpace(field)
		if field != "" && i < len(vals) {
			req.Set[field] = vals[i]
		}
	}
	for _, field := range form["delete"] {
		del(strings.TrimSpace(field))
	}
	for _, field := range strings.FieldsFunc(form.Get("delete_fields"), func(r rune) bool { return r == ',' || r == ' ' }) {
		del(field)
	}
	for _, field := range req.Delete {
		delete(req.Set, field)
	}
	if len(req.Set) == 0 && len(req.Delete) == 0 {
		return modRequest{}, errors.New("nothing to change: no field differs from its current value")
	}
	return req, nil
}

func (s *server) handleCreateModification(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		render(w, http.StatusBadRequest, errorPage(http.StatusBadRequest, err.Error()))
		return
	}
	req, err := modifyRequest(r.PostForm)
	if err != nil {
		render(w, http.StatusUnprocessableEntity, errorPage(http.StatusUnprocessableEntity, err.Error()))
		return
	}
	s.createAndShow(w, r, req)
}

func (s *server) createAndShow(w http.ResponseWriter, r *http.Request, req modRequest) {
	mod, err := s.api.CreateModification(r.Context(), req)
	if err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/modifications/"+url.PathEscape(mod.ID), http.StatusSeeOther)
}

func (s *server) handleModification(w http.ResponseWriter, r *http.Request) {
	mod, err := s.api.Modification(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	render(w, http.StatusOK, modificationView(mod))
}

func (s *server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// A 409 means it was rejected as stale or already confirmed; the
	// Modification page shows which.
	if err := s.api.Confirm(r.Context(), id); err != nil && !isStatus(err, http.StatusConflict) {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/modifications/"+url.PathEscape(id), http.StatusSeeOther)
}

// handleAgain makes a new Modification from an old one's request, for a
// Preview that went stale.
func (s *server) handleAgain(w http.ResponseWriter, r *http.Request) {
	mod, err := s.api.Modification(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	s.createAndShow(w, r, mod.Request)
}

func (s *server) handleHistory(w http.ResponseWriter, r *http.Request) {
	res, err := s.api.Modifications(r.Context(), 50, offsetParam(r))
	p := historyPage{Mods: res.Results, Total: res.Total, Offset: offsetParam(r)}
	if isStatus(err, http.StatusNotFound) || isStatus(err, http.StatusMethodNotAllowed) {
		p.Unavailable = true
	} else if err != nil {
		fail(w, err)
		return
	}
	render(w, http.StatusOK, historyView(p))
}
