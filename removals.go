package main

// Removals: removing Albums or Items, and their files, from the Library. As
// with Modify, beets-api makes a Preview first and nothing is removed until it
// is confirmed.

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// removalQuery reads which records to remove from a form: the ticked rows
// (`id`), or the query that produced the list (`q`).
func removalQuery(form url.Values) string {
	var ids []int
	for _, v := range form["id"] {
		if n, err := strconv.Atoi(v); err == nil {
			ids = append(ids, n)
		}
	}
	if len(ids) > 0 {
		return idsQuery(ids)
	}
	return strings.TrimSpace(form.Get("q"))
}

// handleCreateRemoval asks beets-api for a Preview and shows it. It removes
// nothing: the Preview page asks first.
func (s *server) handleCreateRemoval(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		render(w, http.StatusBadRequest, errorPage(http.StatusBadRequest, err.Error()))
		return
	}
	k, ok := parseScope(r.PostForm.Get("scope"))
	if !ok {
		render(w, http.StatusBadRequest, errorPage(http.StatusBadRequest, "Choose Albums or Items to remove."))
		return
	}
	query := removalQuery(r.PostForm)
	if query == "" {
		render(w, http.StatusBadRequest, errorPage(http.StatusBadRequest, "Nothing selected. Tick some rows, or use “Remove all”."))
		return
	}
	s.createRemovalAndShow(w, r, removalRequest{Scope: k.API(), Query: query})
}

func (s *server) createRemovalAndShow(w http.ResponseWriter, r *http.Request, req removalRequest) {
	rm, err := s.api.CreateRemoval(r.Context(), req)
	if err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/removals/"+url.PathEscape(string(rm.ID)), http.StatusSeeOther)
}

func (s *server) handleRemoval(w http.ResponseWriter, r *http.Request) {
	rm, err := s.api.Removal(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	render(w, http.StatusOK, removalView(rm, rm.Status == "awaiting_confirmation" && s.trashMissing(r)))
}

func (s *server) handleRemovalConfirm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// A 409 means it was rejected as stale or already confirmed, which the
	// Removal page shows; a missing Trash is said outright.
	if err := s.api.ConfirmRemoval(r.Context(), id); err != nil && (isTrashError(err) || !isStatus(err, http.StatusConflict)) {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/removals/"+url.PathEscape(id), http.StatusSeeOther)
}

// handleRemovalAgain makes a new Removal from an old one's request, for a
// Preview that went stale.
func (s *server) handleRemovalAgain(w http.ResponseWriter, r *http.Request) {
	rm, err := s.api.Removal(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	s.createRemovalAndShow(w, r, rm.Request)
}

func (s *server) handleRemovals(w http.ResponseWriter, r *http.Request) {
	res, err := s.api.Removals(r.Context(), 50, offsetParam(r))
	if err != nil {
		fail(w, err)
		return
	}
	render(w, http.StatusOK, removalsView(removalsPage{Removals: res.Results, Total: res.Total, Offset: offsetParam(r)}))
}

// scopeOf is the scope beets-api names "album" or "item".
func scopeOf(api string) scope {
	if api == "album" {
		return albums
	}
	return items
}

// removalCount says what a Removal covers: "2 Albums", "3 Items", or both.
func removalCount(albums_, items_ int) string {
	switch {
	case albums_ > 0 && items_ > 0:
		return count(albums_, albums) + " and " + count(items_, items)
	case albums_ > 0:
		return count(albums_, albums)
	}
	return count(items_, items)
}

func removalPreviewCount(rm removal) string {
	return removalCount(len(rm.Result.Preview.Albums), len(rm.Result.Preview.Items))
}

// removalFileCount is how many files the Preview would delete.
func removalFileCount(rm removal) int {
	n := len(rm.Result.Preview.Items)
	for _, a := range rm.Result.Preview.Albums {
		n += len(a.Items)
		if a.Art != nil {
			n++
		}
	}
	return n
}

func removalProgressStyle(rm removal) string {
	total := len(rm.Result.Preview.Albums) + len(rm.Result.Preview.Items)
	if total == 0 {
		return "width: 0%"
	}
	return "width: " + strconv.Itoa((len(rm.Result.Removed)+len(rm.Result.Failed))*100/total) + "%"
}
