package main

// The Trash and Purges. beets-api moves files here instead of deleting them,
// one folder per operation, and only a Purge deletes from it: a Preview, then
// a confirm, like a Removal. The Trash is off until beets-api is given a
// directory for it, and while it is off nothing that would delete a file works.

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// isTrashError tells whether beets-api refused because the Trash isn't set up.
// That is a 409 like a stale Preview, but retrying can't help, so say why.
func isTrashError(err error) bool {
	return isStatus(err, http.StatusConflict) && strings.Contains(errorMessage(err), "Trash is not configured")
}

// trashMissing tells whether the Trash isn't set up. When beets-api can't say,
// it doesn't claim so: a refusal is shown when it happens.
func (s *server) trashMissing(r *http.Request) bool {
	info, err := s.api.Trash(r.Context())
	return err == nil && !info.Configured
}

func (s *server) handleTrash(w http.ResponseWriter, r *http.Request) {
	info, err := s.api.Trash(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	render(w, http.StatusOK, trashView(info))
}

// handleCreatePurge asks for a Preview of deleting Trash folders: the ticked
// ones (`folder`), those older than some days, or everything. It deletes
// nothing: the Preview page asks first.
func (s *server) handleCreatePurge(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		render(w, http.StatusBadRequest, errorPage(http.StatusBadRequest, err.Error()))
		return
	}
	f := r.PostForm
	var req purgeRequest
	switch {
	case len(f["folder"]) > 0:
		req.Folders = f["folder"]
	case f.Get("older_than_days") != "":
		days, err := strconv.Atoi(f.Get("older_than_days"))
		if err != nil || days < 0 {
			render(w, http.StatusBadRequest, errorPage(http.StatusBadRequest, "Older than how many days?"))
			return
		}
		req.OlderThan = time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	case f.Get("all") == "1":
		// no filters: beets-api means every folder
	default:
		render(w, http.StatusBadRequest, errorPage(http.StatusBadRequest, "Nothing selected. Tick some folders, or choose how much to purge."))
		return
	}
	s.createPurgeAndShow(w, r, req)
}

func (s *server) createPurgeAndShow(w http.ResponseWriter, r *http.Request, req purgeRequest) {
	p, err := s.api.CreatePurge(r.Context(), req)
	if err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/purges/"+url.PathEscape(string(p.ID)), http.StatusSeeOther)
}

func (s *server) handlePurge(w http.ResponseWriter, r *http.Request) {
	p, err := s.api.Purge(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	render(w, http.StatusOK, purgeView(p, p.Status == "awaiting_confirmation" && s.trashMissing(r)))
}

func (s *server) handlePurgeConfirm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// A 409 means it was rejected as stale or already confirmed, which the
	// Purge page shows; a missing Trash is said outright.
	if err := s.api.ConfirmPurge(r.Context(), id); err != nil && (isTrashError(err) || !isStatus(err, http.StatusConflict)) {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/purges/"+url.PathEscape(id), http.StatusSeeOther)
}

// handlePurgeAgain makes a new Purge from an old one's request, for a Preview
// that went stale.
func (s *server) handlePurgeAgain(w http.ResponseWriter, r *http.Request) {
	p, err := s.api.Purge(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	s.createPurgeAndShow(w, r, p.Request)
}

func (s *server) handlePurges(w http.ResponseWriter, r *http.Request) {
	res, err := s.api.Purges(r.Context(), 50, offsetParam(r))
	if err != nil {
		fail(w, err)
		return
	}
	render(w, http.StatusOK, purgesView(purgesPage{Purges: res.Results, Total: res.Total, Offset: offsetParam(r)}))
}

type purgesPage struct {
	Purges []purgeSummary
	Total  int
	Offset int
}

func bytesText(n int64) string { return sizeText(&n) }

// purgeTotals is how many files and bytes a Purge Preview would delete.
func purgeTotals(p purge) (files int, bytes int64) {
	for _, f := range p.Result.Preview.Folders {
		files += f.Files
		bytes += f.Bytes
	}
	return files, bytes
}

// purgeWhat says what a Purge was asked to delete.
func purgeWhat(req purgeRequest) string {
	switch {
	case len(req.Folders) > 0:
		return "Folders " + strings.Join(req.Folders, ", ")
	case req.OlderThan != "":
		return "Everything older than " + msTime(req.OlderThan)
	}
	return "Everything"
}

// folderKind names the operation behind a Trash folder by its audited actions.
func folderKind(f trashFolder) string {
	if f.OperationID == nil {
		return "Art change"
	}
	return "Operation " + strconv.Itoa(*f.OperationID)
}
