package main

// The server and its routes. It signs nobody in: a reverse proxy does, and
// every request must say who it is from (see auth.go).

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const pageSize = 100

type server struct {
	api         *beetsAPI
	art         *artSearch
	pending     pendingArt
	actorHeader string // the header the proxy puts the person's name in
}

func newServer(apiURL string, client *http.Client) *server {
	return &server{api: &beetsAPI{base: strings.TrimRight(apiURL, "/"), http: client}, art: newArtSearch(), actorHeader: defaultActorHeader}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /public/static/", staticHandler())
	mux.HandleFunc("GET /{$}", s.handleHome)
	mux.HandleFunc("GET /library", s.handleLibrary)
	mux.HandleFunc("GET /albums", redirectToLibrary("albums"))
	mux.HandleFunc("GET /items", redirectToLibrary("items"))
	mux.HandleFunc("GET /search", redirectToLibrary(""))
	mux.HandleFunc("GET /albums/{id}", s.handleRecord(albums))
	mux.HandleFunc("GET /items/{id}", s.handleRecord(items))
	mux.HandleFunc("GET /albums/{id}/art", s.handleArt)
	mux.HandleFunc("GET /albums/{id}/art/search", s.handleArtSearch)
	mux.HandleFunc("POST /albums/{id}/art", s.handleSetArt)
	mux.HandleFunc("GET /albums/{id}/art/remove", s.handleArtRemoveForm)
	mux.HandleFunc("POST /albums/{id}/art/remove", s.handleArtRemove)
	mux.HandleFunc("GET /albums/{id}/art/new/{token}", s.handleArtCompare)
	mux.HandleFunc("GET /albums/{id}/art/new/{token}/image", s.handleArtPendingImage)
	mux.HandleFunc("POST /albums/{id}/art/new/{token}", s.handleArtConfirm)
	mux.HandleFunc("GET /modify", s.handleModifyForm)
	mux.HandleFunc("POST /modifications", s.handleCreateModification)
	mux.HandleFunc("GET /modifications", s.handleModifications)
	mux.HandleFunc("POST /removals", s.handleCreateRemoval)
	mux.HandleFunc("GET /removals", s.handleRemovals)
	mux.HandleFunc("GET /removals/{id}", s.handleRemoval)
	mux.HandleFunc("POST /removals/{id}/confirm", s.handleRemovalConfirm)
	mux.HandleFunc("POST /removals/{id}/again", s.handleRemovalAgain)
	mux.HandleFunc("GET /trash", s.handleTrash)
	mux.HandleFunc("POST /purges", s.handleCreatePurge)
	mux.HandleFunc("GET /purges", s.handlePurges)
	mux.HandleFunc("GET /purges/{id}", s.handlePurge)
	mux.HandleFunc("POST /purges/{id}/confirm", s.handlePurgeConfirm)
	mux.HandleFunc("POST /purges/{id}/again", s.handlePurgeAgain)
	mux.HandleFunc("GET /audit", s.handleAudit)
	mux.HandleFunc("GET /audit/{id}", s.handleAuditEntry)
	mux.HandleFunc("GET /profile", s.handleProfile)
	mux.HandleFunc("GET /config", s.handleConfig)
	mux.HandleFunc("GET /modifications/{id}", s.handleModification)
	mux.HandleFunc("POST /modifications/{id}/confirm", s.handleConfirm)
	mux.HandleFunc("POST /modifications/{id}/again", s.handleAgain)
	mux.HandleFunc("GET /import", s.handleImportStart)
	mux.HandleFunc("POST /imports", s.handleCreateImport)
	mux.HandleFunc("GET /imports/{id}", s.handleImport)
	mux.HandleFunc("POST /imports/{id}/choice", s.handleImportChoice)
	mux.HandleFunc("POST /imports/{id}/abort", s.handleImportAbort)
	mux.HandleFunc("GET /imports/{id}/undo", s.handleImportUndo)
	mux.HandleFunc("POST /imports/{id}/undo", s.handleImportUndoConfirm)
	return s.requireActor(mux)
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
// splits and queries Artists.
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
	items:  {"id", "artist", "title", "album", "album_id", "track", "tracktotal", "disc", "disctitle", "length", "format", "bitrate", "trackdisambig", "year", "added"},
}

func filtersFrom(r *http.Request) filters {
	q := r.URL.Query()
	get := func(k string) string { return strings.TrimSpace(q.Get(k)) }
	return filters{
		Query:     get("q"),
		YearFrom:  get("year_from"),
		YearTo:    get("year_to"),
		Artist:    get("artist"),
		Genre:     get("genre"),
		AlbumType: get("albumtype"),
		Format:    get("format"),
	}
}

// recentAlbumCount is how many recently added Albums the home page shows.
const recentAlbumCount = 12

// handleHome shows the search bar and the newest Albums. The page works
// without them: if beets-api can't list Albums, only the search bar shows.
func (s *server) handleHome(w http.ResponseWriter, r *http.Request) {
	res, err := s.api.List(r.Context(), albums, sortQuery("", albums, "recent"), recentAlbumCount, 0, listFields[albums])
	if err != nil {
		log.Printf("home: recent albums: %v", err)
	}
	render(w, http.StatusOK, homeView(res.Results))
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
		tab := r.URL.Query().Get("tab")
		p, err := s.recordPage(r.Context(), k, id, tab)
		if err != nil {
			fail(w, err)
			return
		}
		switch {
		case r.URL.Query().Get("set") == "1":
			p.ArtNotice = "Art set."
		case r.URL.Query().Get("removed") == "1":
			p.ArtNotice = "Art removed."
		}
		render(w, http.StatusOK, recordView(p))
	}
}

// recordPage loads a record page. An Album page has the tabs items, fields
// and art; an Item page only shows its fields.
func (s *server) recordPage(ctx context.Context, k scope, id int, tab string) (recordPage, error) {
	rec, err := s.api.Get(ctx, k, id)
	if err != nil {
		return recordPage{}, err
	}
	p := recordPage{Scope: k, Tab: "items", Record: rec, Separators: s.separators(ctx)}
	if k == items || tab == "fields" {
		p.Tab = "fields"
	} else if tab == "art" {
		p.Tab = "art"
	}
	if k == albums {
		res, err := s.api.List(ctx, items, "album_id:"+strconv.Itoa(id)+" disc+ track+", 1000, 0, listFields[items])
		if err != nil {
			return recordPage{}, err
		}
		p.Items = tableData{Scope: items, Query: "album_id:" + strconv.Itoa(id), Rows: res.Results, Total: res.Total, Separators: p.Separators}
		p.Items.Discs = discTables(res.Results)
		p.Info = albumInfoOf(rec, res.Results, p.Items.Discs)
		p.Items.ShowArtist = p.Info.ShowArtist
		if p.Tab == "art" {
			p.Art = s.currentArt(ctx, id)
			p.ArtQuery = strings.TrimSpace(rec.Str("albumartist") + " " + rec.Str("album"))
			p.ArtPane = "search"
		}
	} else if albumID := rec.Int("album_id"); albumID != 0 {
		if album, err := s.api.Get(ctx, albums, albumID); err == nil {
			p.Album = album
		}
	}
	return p, nil
}

// currentArt is an Album's art as beets-api serves it, if it has any.
func (s *server) currentArt(ctx context.Context, id int) artInfo {
	resp, err := s.api.AlbumArt(ctx, id)
	if err != nil {
		return artInfo{}
	}
	defer resp.Body.Close()
	w, h := imageSize(resp.Body)
	return artInfo{Exists: true, Width: w, Height: h}
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
	for _, h := range []string{"Content-Type", "Content-Length", "ETag", "Last-Modified"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	// Set art keeps the same URL, so the browser must check it each time.
	w.Header().Set("Cache-Control", "no-cache")
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
	http.Redirect(w, r, "/modifications/"+url.PathEscape(string(mod.ID)), http.StatusSeeOther)
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

func (s *server) handleModifications(w http.ResponseWriter, r *http.Request) {
	res, err := s.api.Modifications(r.Context(), 50, offsetParam(r))
	if err != nil {
		fail(w, err)
		return
	}
	render(w, http.StatusOK, modificationsView(modificationsPage{Mods: res.Results, Total: res.Total, Offset: offsetParam(r)}))
}

// handleAuditEntry shows one Audit entry whole.
func (s *server) handleAuditEntry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		fail(w, &apiError{Status: http.StatusNotFound, Detail: "audit entry not found"})
		return
	}
	e, err := s.api.AuditEntry(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	// The other entries for the same Album or Item, newest first.
	history, err := s.api.Audit(r.Context(), e.Target, nil, 0, "", 20, 0)
	if err != nil {
		fail(w, err)
		return
	}
	render(w, http.StatusOK, auditEntryView(auditEntryPage{Entry: e, History: history.Results, HistoryTotal: history.Total}))
}

func (s *server) handleAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p := auditPage{Target: strings.TrimSpace(q.Get("target")), Action: q.Get("action"), Actor: strings.TrimSpace(q.Get("actor")), Offset: offsetParam(r)}
	if n, err := strconv.Atoi(q.Get("operation")); err == nil && n > 0 {
		p.Operation = n
	}
	var actions []string
	if p.Action != "" {
		actions = []string{p.Action}
	}
	res, err := s.api.Audit(r.Context(), p.Target, actions, p.Operation, p.Actor, 50, p.Offset)
	if err != nil {
		fail(w, err)
		return
	}
	p.Entries, p.Total = res.Results, res.Total
	render(w, http.StatusOK, auditView(p))
}

// handleArtSearch renders candidate art for the Art tab, loaded by htmx.
func (s *server) handleArtSearch(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		render(w, http.StatusOK, artResults(id, nil, []string{"Enter something to search for."}))
		return
	}
	ctx := r.Context()
	found, problems := s.art.Search(ctx, q)
	rankArt(found, s.artTarget(ctx, id))
	render(w, http.StatusOK, artResults(id, found, problems))
}

// artTarget is what search results are matched against. If beets-api fails,
// the zero target still ranks by size.
func (s *server) artTarget(ctx context.Context, id int) artTarget {
	rec, err := s.api.Get(ctx, albums, id)
	if err != nil {
		return artTarget{}
	}
	t := artTarget{Artist: rec.Str("albumartist"), Title: rec.Str("album"), Year: rec.Int("year")}
	if res, err := s.api.List(ctx, items, "album_id:"+strconv.Itoa(id), 1, 0, []string{"id"}); err == nil {
		t.Tracks = res.Total
	}
	return t
}

// handleSetArt takes an image from a URL (a search result's or one entered)
// or an uploaded file. An Album without art gets it straight away; one with
// art goes to the comparison first.
func (s *server) handleSetArt(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var data []byte
	var source string
	var err error
	if err = r.ParseMultipartForm(maxArtBytes + 1<<20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		err = errors.New("the upload is too large or broken")
	} else if file, header, ferr := r.FormFile("file"); ferr == nil {
		defer file.Close()
		source = header.Filename
		data, err = readImage(file)
	} else if u := strings.TrimSpace(r.FormValue("url")); u != "" {
		source = u
		data, err = s.art.Fetch(ctx, u)
	} else {
		err = errors.New("choose an image, enter a URL or pick a file")
	}
	if err != nil {
		s.artError(w, r, id, err.Error())
		return
	}
	rec, err := s.api.Get(ctx, albums, id)
	if err != nil {
		fail(w, err)
		return
	}
	if rec.Str("artpath") != "" {
		token := s.pending.put(id, data, source)
		http.Redirect(w, r, "/albums/"+strconv.Itoa(id)+"/art/new/"+token, http.StatusSeeOther)
		return
	}
	s.setArt(w, r, id, data)
}

func (s *server) setArt(w http.ResponseWriter, r *http.Request, id int, data []byte) {
	if err := s.api.SetAlbumArt(r.Context(), id, data); err != nil {
		var e *apiError
		if errors.As(err, &e) && e.Status != http.StatusNotFound && e.Status < 500 {
			s.artError(w, r, id, e.Detail)
			return
		}
		fail(w, err)
		return
	}
	http.Redirect(w, r, recordTabURL(albums, id, "art")+"&set=1", http.StatusSeeOther)
}

// artError shows the Art tab again with what went wrong.
func (s *server) artError(w http.ResponseWriter, r *http.Request, id int, msg string) {
	p, err := s.recordPage(r.Context(), albums, id, "art")
	if err != nil {
		fail(w, err)
		return
	}
	p.ArtError = sentence(msg)
	// Reopen the way you tried: a search result, a URL or a file.
	switch {
	case r.FormValue("candidate") != "":
	case r.MultipartForm != nil && len(r.MultipartForm.File["file"]) > 0:
		p.ArtPane = "file"
	default:
		p.ArtPane = "url"
		p.ArtURL = r.FormValue("url")
	}
	render(w, http.StatusUnprocessableEntity, recordView(p))
}

func (s *server) pendingParams(w http.ResponseWriter, r *http.Request) (int, string, pendingImage, bool) {
	id, ok := idParam(w, r)
	if !ok {
		return 0, "", pendingImage{}, false
	}
	token := r.PathValue("token")
	img, ok := s.pending.get(id, token)
	if !ok {
		render(w, http.StatusNotFound, errorPage(http.StatusNotFound, "That image is no longer waiting to be set. Choose it again from the Art tab."))
		return 0, "", pendingImage{}, false
	}
	return id, token, img, true
}

func (s *server) handleArtCompare(w http.ResponseWriter, r *http.Request) {
	id, token, img, ok := s.pendingParams(w, r)
	if !ok {
		return
	}
	rec, err := s.api.Get(r.Context(), albums, id)
	if err != nil {
		fail(w, err)
		return
	}
	nw, nh := img.size()
	render(w, http.StatusOK, artCompareView(artComparePage{
		Album:   rec,
		Token:   token,
		Current: s.currentArt(r.Context(), id),
		New:     artInfo{Exists: true, Width: nw, Height: nh},
		Source:  img.source,
	}))
}

func (s *server) handleArtPendingImage(w http.ResponseWriter, r *http.Request) {
	_, _, img, ok := s.pendingParams(w, r)
	if !ok {
		return
	}
	t, _ := imageType(img.data)
	w.Header().Set("Content-Type", t)
	w.Write(img.data)
}

func (s *server) handleArtConfirm(w http.ResponseWriter, r *http.Request) {
	id, token, img, ok := s.pendingParams(w, r)
	if !ok {
		return
	}
	s.pending.remove(token)
	s.setArt(w, r, id, img.data)
}

// sentence capitalises the first letter of an error for display.
func sentence(s string) string {
	if s == "" {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

// handleArtRemoveForm asks before removing an Album's art.
func (s *server) handleArtRemoveForm(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	rec, err := s.api.Get(r.Context(), albums, id)
	if err != nil {
		fail(w, err)
		return
	}
	if rec.Str("artpath") == "" {
		http.Redirect(w, r, recordTabURL(albums, id, "art"), http.StatusSeeOther)
		return
	}
	render(w, http.StatusOK, artRemoveView(rec, s.currentArt(r.Context(), id)))
}

func (s *server) handleArtRemove(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := s.api.RemoveAlbumArt(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, recordTabURL(albums, id, "art")+"&removed=1", http.StatusSeeOther)
}
