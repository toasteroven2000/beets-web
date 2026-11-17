package main

// The client for beets-api. The UI reads and changes the Library only through
// it, never through the beets database.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// record is an Album or Item as beets-api returns it. Numbers stay as
// json.Number so they print exactly as beets stored them.
type record map[string]any

func (r record) ID() int {
	n, _ := r["id"].(json.Number).Int64()
	return int(n)
}

// Str is a field's value as text, the way the UI shows it and a Modify sets it.
func (r record) Str(key string) string { return valueString(r[key]) }

func (r record) Int(key string) int {
	n, ok := r[key].(json.Number)
	if !ok {
		return 0
	}
	i, err := n.Int64()
	if err != nil {
		f, _ := n.Float64()
		return int(f)
	}
	return int(i)
}

// valueString renders a JSON value from beets-api. Lists are joined with "; ",
// the separator `beet modify` splits multi-valued fields on.
func valueString(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	case []any:
		parts := make([]string, len(v))
		for i, p := range v {
			parts[i] = valueString(p)
		}
		return strings.Join(parts, "; ")
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

type listResult struct {
	Total   int      `json:"total"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
	Results []record `json:"results"`
}

// modRequest is a Modify as beets-api takes it. Scope is "item" or "album".
type modRequest struct {
	Scope  string            `json:"scope"`
	Query  string            `json:"query"`
	Set    map[string]string `json:"set"`
	Delete []string          `json:"delete"`
}

// scope is which of Albums or Items the request matches.
func (r modRequest) scope() scope {
	if r.Scope == "album" {
		return albums
	}
	return items
}

type change struct {
	Old any `json:"old"`
	New any `json:"new"`
}

type previewEntry struct {
	ID      int               `json:"id"`
	Label   string            `json:"label"`
	Changes map[string]change `json:"changes"`
}

type ref struct {
	Scope string `json:"kind"` // "albums" or "items"
	ID    int    `json:"id"`
	Path  string `json:"path,omitempty"`
	Error string `json:"error,omitempty"`
}

type modResult struct {
	Write   bool `json:"write"`
	Move    bool `json:"move"`
	Preview struct {
		Albums []previewEntry `json:"albums"`
		Items  []previewEntry `json:"items"`
	} `json:"preview"`
	Applied []ref  `json:"applied"`
	Failed  []ref  `json:"failed"`
	Current *ref   `json:"current"`
	Stale   []ref  `json:"stale"`
	Error   string `json:"error"`
}

// opID is an operation id. beets-api sends integers now and sent strings
// before; the UI only ever passes it back.
type opID string

func (o *opID) UnmarshalJSON(b []byte) error {
	var n json.Number
	if err := json.Unmarshal(b, &n); err == nil {
		*o = opID(n.String())
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*o = opID(s)
	return nil
}

type modification struct {
	ID        opID       `json:"id"`
	Status    string     `json:"status"`
	Request   modRequest `json:"request"`
	Result    modResult  `json:"result"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
}

// modSummary is one row of the Modifications list, without the Preview.
type modSummary struct {
	ID        opID       `json:"id"`
	Status    string     `json:"status"`
	Request   modRequest `json:"request"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
}

type modList struct {
	Total   int          `json:"total"`
	Results []modSummary `json:"results"`
}

// auditEntry is one change beets-api made to one Album or Item.
type auditEntry struct {
	ID          int            `json:"id"`
	RecordedAt  string         `json:"recorded_at"`
	Client      string         `json:"client"`
	Actor       string         `json:"actor"`  // the person the Client acted for
	Action      string         `json:"action"` // e.g. album.modify, art.upload
	Target      string         `json:"target"` // e.g. album:5
	OperationID *int           `json:"operation_id"`
	Changes     map[string]any `json:"changes"`
}

type auditList struct {
	Total   int          `json:"total"`
	Results []auditEntry `json:"results"`
}

// apiError is a non-2xx answer from beets-api, with its `detail` as text.
type apiError struct {
	Status int
	Detail string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("beets-api: %d %s", e.Status, e.Detail)
}

func isStatus(err error, status int) bool {
	var e *apiError
	return errors.As(err, &e) && e.Status == status
}

type beetsAPI struct {
	base string
	http *http.Client
}

// clientName is sent as X-Beets-API-Client, so beets-api's audit shows which
// changes came through the UI.
const clientName = "beets-web"

// stamp says who is calling beets-api: this program, and the person it acts
// for, whose name beets-api records in its Audit.
func stamp(ctx context.Context, req *http.Request) {
	req.Header.Set("X-Beets-API-Client", clientName)
	if who := currentUser(ctx); who.Actor != "" {
		req.Header.Set("X-Beets-API-Actor", who.Actor)
	}
}

func (c *beetsAPI) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	stamp(ctx, req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("beets-api unreachable: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return &apiError{Status: resp.StatusCode, Detail: detailText(data)}
	}
	if out == nil {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(out)
}

// detailText pulls FastAPI's `detail` out of an error body, which may be a
// string, an object holding its own `detail`, or a list of validation errors.
func detailText(body []byte) string {
	var wrapper struct {
		Detail any `json:"detail"`
	}
	if json.Unmarshal(body, &wrapper) != nil || wrapper.Detail == nil {
		return strings.TrimSpace(string(body))
	}
	switch d := wrapper.Detail.(type) {
	case string:
		return d
	case map[string]any:
		if s, ok := d["detail"].(string); ok {
			return s
		}
	case []any:
		var msgs []string
		for _, e := range d {
			if m, ok := e.(map[string]any); ok {
				if s, ok := m["msg"].(string); ok {
					msgs = append(msgs, s)
				}
			}
		}
		if len(msgs) > 0 {
			return strings.Join(msgs, "; ")
		}
	}
	b, _ := json.Marshal(wrapper.Detail)
	return string(b)
}

func (c *beetsAPI) List(ctx context.Context, s scope, query string, limit, offset int, fields []string) (listResult, error) {
	v := url.Values{
		"query":  {query},
		"scope":  {s.API()},
		"limit":  {strconv.Itoa(limit)},
		"offset": {strconv.Itoa(offset)},
	}
	if len(fields) > 0 {
		v.Set("fields", strings.Join(fields, ","))
	}
	var out listResult
	err := c.do(ctx, http.MethodGet, "/list", v, nil, &out)
	return out, err
}

func (c *beetsAPI) Get(ctx context.Context, s scope, id int) (record, error) {
	var out record
	err := c.do(ctx, http.MethodGet, "/"+string(s)+"/"+strconv.Itoa(id), nil, nil, &out)
	return out, err
}

func (c *beetsAPI) Config(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodGet, "/config", nil, nil, &out)
	return out, err
}

func (c *beetsAPI) CreateModification(ctx context.Context, req modRequest) (modification, error) {
	req.Scope = req.scope().API()
	if req.Set == nil {
		req.Set = map[string]string{}
	}
	if req.Delete == nil {
		req.Delete = []string{}
	}
	var out modification
	err := c.do(ctx, http.MethodPost, "/modifications", nil, req, &out)
	return out, err
}

func (c *beetsAPI) Modification(ctx context.Context, id string) (modification, error) {
	var out modification
	err := c.do(ctx, http.MethodGet, "/modifications/"+url.PathEscape(id), nil, nil, &out)
	return out, err
}

func (c *beetsAPI) Confirm(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/modifications/"+url.PathEscape(id)+"/confirm", nil, nil, nil)
}

func (c *beetsAPI) Modifications(ctx context.Context, limit, offset int) (modList, error) {
	v := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var out modList
	err := c.do(ctx, http.MethodGet, "/modifications", v, nil, &out)
	return out, err
}

// AlbumArt fetches an Album's art. Callers treat any error as "no art". The
// browser's If-None-Match and If-Modified-Since (from conditional, may be nil)
// are passed on, so an unchanged image comes back as 304 Not Modified with no
// body.
func (c *beetsAPI) AlbumArt(ctx context.Context, id int, conditional http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/albums/"+strconv.Itoa(id)+"/art", nil)
	if err != nil {
		return nil, err
	}
	for _, h := range []string{"If-None-Match", "If-Modified-Since"} {
		if v := conditional.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotModified {
		resp.Body.Close()
		return nil, &apiError{Status: resp.StatusCode}
	}
	return resp, nil
}

// Audio fetches a file for playing: an Item's (path "/items/7/audio") or one
// under an Import Path ("/import-paths/audio" with the path). The Range header
// is passed on so a browser can seek, and the response is returned as it came,
// whatever its status, for the caller to relay.
func (c *beetsAPI) Audio(ctx context.Context, path string, query url.Values, rangeHeader string) (*http.Response, error) {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	return c.http.Do(req)
}

// SetAlbumArt sends an image (JPEG or PNG bytes) as an Album's art.
func (c *beetsAPI) SetAlbumArt(ctx context.Context, id int, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.base+"/albums/"+strconv.Itoa(id)+"/art", bytes.NewReader(data))
	if err != nil {
		return err
	}
	if t, ok := imageType(data); ok {
		req.Header.Set("Content-Type", t)
	}
	stamp(ctx, req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("beets-api unreachable: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return &apiError{Status: resp.StatusCode, Detail: detailText(body)}
	}
	return nil
}

// RemoveAlbumArt clears an Album's art. beets-api moves the image to the Trash.
func (c *beetsAPI) RemoveAlbumArt(ctx context.Context, id int) error {
	return c.do(ctx, http.MethodDelete, "/albums/"+strconv.Itoa(id)+"/art", nil, nil, nil)
}

// Audit lists changes newest first, optionally for one target ("album:5"),
// some actions, or one Modification or Import (operation > 0).
func (c *beetsAPI) Audit(ctx context.Context, target string, actions []string, operation int, actor string, limit, offset int) (auditList, error) {
	v := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	if target != "" {
		v.Set("target", target)
	}
	if operation > 0 {
		v.Set("operation_id", strconv.Itoa(operation))
	}
	if actor != "" {
		v.Set("actor", actor)
	}
	for _, a := range actions {
		v.Add("action", a)
	}
	var out auditList
	err := c.do(ctx, http.MethodGet, "/audit", v, nil, &out)
	return out, err
}

// importEntry is a directory or file inside an Import Path.
type importEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Type  string `json:"type"` // "dir" or "file"
	MTime string `json:"mtime"`
	Size  *int64 `json:"size"`
}

// importBrowse is one directory's children, or the Import Paths themselves
// when Path is nil.
type importBrowse struct {
	Path    *string       `json:"path"`
	Parent  *string       `json:"parent"`
	Total   int           `json:"total"` // children of the folder, of which Entries is a page
	Limit   int           `json:"limit"`
	Offset  int           `json:"offset"`
	Entries []importEntry `json:"entries"`
}

type importRequest struct {
	Paths      []string `json:"paths"`
	Singletons bool     `json:"singletons"`
}

// fileChanges are the field changes applying a Candidate makes to one file.
type fileChanges struct {
	Path    string            `json:"path"`
	Changes map[string]change `json:"changes"`
	Fields  map[string]change `json:"fields"` // shown fields, changed or not
}

// candidate is a possible match for an Import Task: an album match has
// Album and Tracks, a track match has Title and Track.
type candidate struct {
	Index       int           `json:"index"`
	Distance    float64       `json:"distance"`
	Artist      string        `json:"artist"`
	Album       string        `json:"album"`
	Title       string        `json:"title"`
	Year        any           `json:"year"`
	ID          string        `json:"id"`
	DataSource  string        `json:"data_source"`
	Tracks      []fileChanges `json:"tracks"`
	Track       *fileChanges  `json:"track"`
	ExtraItems  []string      `json:"extra_items"`
	ExtraTracks []any         `json:"extra_tracks"`
}

// importPrompt is the Choice an Import is waiting for. Kind is "album",
// "item" or "duplicate".
type importPrompt struct {
	ID             string            `json:"id"`
	Kind           string            `json:"kind"`
	Paths          []string          `json:"paths"`
	Choices        []string          `json:"choices"`
	Current        map[string]string `json:"current"`
	Recommendation string            `json:"recommendation"`
	Candidates     []candidate       `json:"candidates"`
	Existing       []record          `json:"existing"`
	// Known are what the Library already has under the names this prompt could
	// lead to, by artist and name: beets only says a match is a duplicate after
	// it is chosen, so the page looks ahead. Not from beets-api.
	Known map[string][]record `json:"-"`
}

// importChoice is a Choice already made, with the prompt it answered.
type importChoice struct {
	Paths     []string `json:"paths"`
	Kind      string   `json:"kind"`
	Action    string   `json:"action"`
	Candidate *int     `json:"candidate"`
	Artist    *string  `json:"artist"`
	Name      *string  `json:"name"`
	SearchIDs []string `json:"search_ids"`
	Actor     *string  `json:"actor"` // who gave this Choice
}

// importOutcome is what became of one Import Task.
type importOutcome struct {
	Paths   []string `json:"paths"`
	Skipped bool     `json:"skipped"`
	Items   []record `json:"items"`
}

type importResult struct {
	Prompt   *importPrompt   `json:"prompt"`
	Choices  []importChoice  `json:"choices"`
	Outcomes []importOutcome `json:"outcomes"`
	Error    string          `json:"error"`
	Actor    *string         `json:"actor"` // who started the Import
	// ExpectedTasks is how many Import Tasks beets will ask about, counted when
	// the Import starts: nil if it couldn't be, and only an estimate, as choosing
	// "Import as tracks" or "Group albums" adds some. TasksDone is how many it
	// has finished, skipped ones too.
	ExpectedTasks *int `json:"expected_tasks"`
	TasksDone     int  `json:"tasks_done"`
	Undone        *struct {
		At string `json:"at"`
	} `json:"undone"` // set once the Import has been undone
	UndoError string `json:"undo_error"` // why an undo that started didn't finish
}

// importOp is one Import. Status is running, waiting_for_choice, completed,
// aborted or failed.
type importOp struct {
	ID        opID          `json:"id"`
	Status    string        `json:"status"`
	Request   importRequest `json:"request"`
	Result    importResult  `json:"result"`
	Counts    *importCounts `json:"counts"`  // only in the Imports list
	Undoing   bool          `json:"undoing"` // beets-api is moving the Import's files to the Trash
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`
}

// importCounts summarises an Import in the list, which leaves out its prompt.
type importCounts struct {
	Choices int `json:"choices"`
	Tasks   int `json:"tasks"`
	Skipped int `json:"skipped"`
	Items   int `json:"items"`
}

type importList struct {
	Total   int        `json:"total"`
	Results []importOp `json:"results"`
}

// choiceRequest answers an Import's prompt.
type choiceRequest struct {
	PromptID  string   `json:"prompt_id"`
	Action    string   `json:"action"`
	Candidate *int     `json:"candidate"`
	Artist    *string  `json:"artist"`
	Name      *string  `json:"name"`
	SearchIDs []string `json:"search_ids"`
}

// ImportPaths lists the Import Paths, or with a path, that directory's children.
func (c *beetsAPI) ImportPaths(ctx context.Context, path string, offset int) (importBrowse, error) {
	v := url.Values{"limit": {strconv.Itoa(importEntriesPerPage)}, "offset": {strconv.Itoa(offset)}}
	if path != "" {
		v.Set("path", path)
	}
	var out importBrowse
	err := c.do(ctx, http.MethodGet, "/import-paths", v, nil, &out)
	return out, err
}

func (c *beetsAPI) StartImport(ctx context.Context, req importRequest) (importOp, error) {
	var out importOp
	err := c.do(ctx, http.MethodPost, "/imports", nil, req, &out)
	return out, err
}

func (c *beetsAPI) Import(ctx context.Context, id string) (importOp, error) {
	var out importOp
	err := c.do(ctx, http.MethodGet, "/imports/"+url.PathEscape(id), nil, nil, &out)
	return out, err
}

func (c *beetsAPI) ImportChoice(ctx context.Context, id string, req choiceRequest) error {
	if req.SearchIDs == nil {
		req.SearchIDs = []string{}
	}
	return c.do(ctx, http.MethodPost, "/imports/"+url.PathEscape(id)+"/choice", nil, req, nil)
}

func (c *beetsAPI) AbortImport(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/imports/"+url.PathEscape(id), nil, nil, nil)
}

// AuditEntry is one Audit entry by id; a 404 means there is none.
func (c *beetsAPI) AuditEntry(ctx context.Context, id int) (auditEntry, error) {
	var out auditEntry
	err := c.do(ctx, http.MethodGet, "/audit/"+strconv.Itoa(id), nil, nil, &out)
	return out, err
}

// Imports lists Imports newest first, optionally only those with a status.
func (c *beetsAPI) Imports(ctx context.Context, statuses []string, limit, offset int) (importList, error) {
	v := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	for _, s := range statuses {
		v.Add("status", s)
	}
	var out importList
	err := c.do(ctx, http.MethodGet, "/imports", v, nil, &out)
	return out, err
}

// undoPlan is what undoing an Import would remove, and why it may not be done.
type undoPlan struct {
	ImportID     int          `json:"import_id"`
	Mode         string       `json:"mode"`
	Allowed      bool         `json:"allowed"`
	Reasons      []string     `json:"reasons"`
	Albums       []undoAlbum  `json:"albums"`
	Items        []undoItem   `json:"items"`
	ChangesSince []undoChange `json:"changes_since"`
}

type undoAlbum struct {
	ID          int        `json:"id"`
	AlbumArtist string     `json:"albumartist"`
	Album       string     `json:"album"`
	Items       []undoItem `json:"items"`
	Art         *string    `json:"art"`
}

type undoItem struct {
	ID     int    `json:"id"`
	Artist string `json:"artist"`
	Title  string `json:"title"`
	Path   string `json:"path"`
}

// undoChange is a later Audit entry for something the Import added.
type undoChange struct {
	ID         int    `json:"id"`
	RecordedAt string `json:"recorded_at"`
	Action     string `json:"action"`
	Target     string `json:"target"`
}

func (c *beetsAPI) UndoPreview(ctx context.Context, id string) (undoPlan, error) {
	var out undoPlan
	err := c.do(ctx, http.MethodGet, "/imports/"+url.PathEscape(id)+"/undo", nil, nil, &out)
	return out, err
}

func (c *beetsAPI) UndoImport(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/imports/"+url.PathEscape(id)+"/undo", nil, nil, nil)
}

// removalRequest is a Remove as beets-api takes it: a scope and a query.
type removalRequest struct {
	Scope string `json:"scope"`
	Query string `json:"query"`
}

// removalFile is one Item of an Album that would be removed, and its file.
type removalFile struct {
	ID   int    `json:"id"`
	Path string `json:"path"`
}

type removalAlbum struct {
	ID    int           `json:"id"`
	Label string        `json:"label"`
	Items []removalFile `json:"items"`
	Art   *string       `json:"art"`
}

type removalItem struct {
	ID      int    `json:"id"`
	Label   string `json:"label"`
	Path    string `json:"path"`
	AlbumID *int   `json:"album_id"`
}

type removalResult struct {
	Preview struct {
		Albums []removalAlbum `json:"albums"`
		Items  []removalItem  `json:"items"`
	} `json:"preview"`
	Removed []ref  `json:"removed"`
	Failed  []ref  `json:"failed"`
	Current *ref   `json:"current"`
	Stale   []ref  `json:"stale"`
	Error   string `json:"error"`
}

// removal is one requested Remove, from its Preview to its outcome.
type removal struct {
	ID        opID           `json:"id"`
	Status    string         `json:"status"`
	Request   removalRequest `json:"request"`
	Result    removalResult  `json:"result"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

// removalSummary is one row of the Removals list, without the Preview.
type removalSummary struct {
	ID      opID           `json:"id"`
	Status  string         `json:"status"`
	Request removalRequest `json:"request"`
	Counts  struct {
		Albums  int `json:"albums"`
		Items   int `json:"items"`
		Removed int `json:"removed"`
		Failed  int `json:"failed"`
	} `json:"counts"`
	CreatedAt string `json:"created_at"`
}

type removalList struct {
	Total   int              `json:"total"`
	Results []removalSummary `json:"results"`
}

func (c *beetsAPI) CreateRemoval(ctx context.Context, req removalRequest) (removal, error) {
	var out removal
	err := c.do(ctx, http.MethodPost, "/removals", nil, req, &out)
	return out, err
}

func (c *beetsAPI) Removal(ctx context.Context, id string) (removal, error) {
	var out removal
	err := c.do(ctx, http.MethodGet, "/removals/"+url.PathEscape(id), nil, nil, &out)
	return out, err
}

func (c *beetsAPI) ConfirmRemoval(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/removals/"+url.PathEscape(id)+"/confirm", nil, nil, nil)
}

func (c *beetsAPI) Removals(ctx context.Context, limit, offset int) (removalList, error) {
	v := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var out removalList
	err := c.do(ctx, http.MethodGet, "/removals", v, nil, &out)
	return out, err
}

// trashFolder is one folder of the Trash: what one operation moved there.
type trashFolder struct {
	Name        string   `json:"name"`
	Files       int      `json:"files"`
	Bytes       int64    `json:"bytes"`
	Modified    string   `json:"modified"`
	Fingerprint string   `json:"fingerprint"`
	OperationID *int     `json:"operation_id"`
	Actions     []string `json:"actions"`
	Actors      []string `json:"actors"`
}

// trashInfo is the Trash. Until beets-api is given a directory for it, it
// refuses everything that would delete a file.
type trashInfo struct {
	Configured bool          `json:"configured"`
	Folders    []trashFolder `json:"folders"`
}

type purgeRequest struct {
	Folders   []string `json:"folders,omitempty"`
	OlderThan string   `json:"older_than,omitempty"`
}

type purgeResult struct {
	Preview struct {
		Folders []trashFolder `json:"folders"`
	} `json:"preview"`
	TrashConfigured bool     `json:"trash_configured"`
	Purged          []string `json:"purged"`
	Failed          []struct {
		Name  string `json:"name"`
		Error string `json:"error"`
	} `json:"failed"`
	Stale []string `json:"stale"`
	Error string   `json:"error"`
}

// purge is one requested deletion of Trash folders, from its Preview to its outcome.
type purge struct {
	ID        opID         `json:"id"`
	Status    string       `json:"status"`
	Request   purgeRequest `json:"request"`
	Result    purgeResult  `json:"result"`
	CreatedAt string       `json:"created_at"`
}

type purgeSummary struct {
	ID      opID         `json:"id"`
	Status  string       `json:"status"`
	Request purgeRequest `json:"request"`
	Counts  struct {
		Folders int `json:"folders"`
		Purged  int `json:"purged"`
		Failed  int `json:"failed"`
	} `json:"counts"`
	CreatedAt string `json:"created_at"`
}

type purgeList struct {
	Total   int            `json:"total"`
	Results []purgeSummary `json:"results"`
}

func (c *beetsAPI) Trash(ctx context.Context) (trashInfo, error) {
	var out trashInfo
	err := c.do(ctx, http.MethodGet, "/trash", nil, nil, &out)
	return out, err
}

func (c *beetsAPI) CreatePurge(ctx context.Context, req purgeRequest) (purge, error) {
	var out purge
	err := c.do(ctx, http.MethodPost, "/purges", nil, req, &out)
	return out, err
}

func (c *beetsAPI) Purge(ctx context.Context, id string) (purge, error) {
	var out purge
	err := c.do(ctx, http.MethodGet, "/purges/"+url.PathEscape(id), nil, nil, &out)
	return out, err
}

func (c *beetsAPI) ConfirmPurge(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/purges/"+url.PathEscape(id)+"/confirm", nil, nil, nil)
}

func (c *beetsAPI) Purges(ctx context.Context, limit, offset int) (purgeList, error) {
	v := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var out purgeList
	err := c.do(ctx, http.MethodGet, "/purges", v, nil, &out)
	return out, err
}

// importTask is one Import Task beets expects: the folder (or file) it is
// about, and where it is: done, current or pending.
type importTask struct {
	Paths []string `json:"paths"`
	State string   `json:"state"`
}

type importTaskList struct {
	Total   int          `json:"total"`
	Results []importTask `json:"results"`
}

// ImportTasks lists the Tasks an active Import expects, in the order beets will
// ask. beets-api has none for an Import that has ended.
func (c *beetsAPI) ImportTasks(ctx context.Context, id string, states []string, limit, offset int) (importTaskList, error) {
	v := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	for _, s := range states {
		v.Add("state", s)
	}
	var out importTaskList
	err := c.do(ctx, http.MethodGet, "/imports/"+url.PathEscape(id)+"/tasks", v, nil, &out)
	return out, err
}
