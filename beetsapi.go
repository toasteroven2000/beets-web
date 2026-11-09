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

type artistSummary struct {
	Name       string `json:"name"`
	AlbumCount int    `json:"album_count"`
	ItemCount  int    `json:"item_count"`
}

type artistsResult struct {
	Total   int             `json:"total"`
	Results []artistSummary `json:"results"`
}

// modRequest is a Modify as beets-api takes it. Scope is "item" or "album";
// Modifications made before beets-api had Scope recorded `album` instead.
type modRequest struct {
	Scope       string            `json:"scope"`
	Query       string            `json:"query"`
	Set         map[string]string `json:"set"`
	Delete      []string          `json:"delete"`
	LegacyAlbum *bool             `json:"album,omitempty"`
}

// scope is which of Albums or Items the request matches.
func (r modRequest) scope() scope {
	if r.Scope == "album" || (r.Scope == "" && r.LegacyAlbum != nil && *r.LegacyAlbum) {
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
	Scope string `json:"scope"` // "albums" or "items"
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

type modification struct {
	ID        string     `json:"id"`
	Status    string     `json:"status"`
	Request   modRequest `json:"request"`
	Result    modResult  `json:"result"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
}

// modSummary is one row of the history, without the Preview.
type modSummary struct {
	ID        string     `json:"id"`
	Status    string     `json:"status"`
	Request   modRequest `json:"request"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
}

type modList struct {
	Total   int          `json:"total"`
	Results []modSummary `json:"results"`
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

func (c *beetsAPI) Artists(ctx context.Context, query string) (artistsResult, error) {
	var out artistsResult
	v := url.Values{}
	if query != "" {
		v.Set("query", query)
	}
	err := c.do(ctx, http.MethodGet, "/artists", v, nil, &out)
	return out, err
}

func (c *beetsAPI) CreateModification(ctx context.Context, req modRequest) (modification, error) {
	req.Scope = req.scope().API()
	req.LegacyAlbum = nil
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

// AlbumArt fetches an Album's art. Not every beets-api has the endpoint yet,
// so callers treat any error as "no art".
func (c *beetsAPI) AlbumArt(ctx context.Context, id int) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/albums/"+strconv.Itoa(id)+"/art", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, &apiError{Status: resp.StatusCode}
	}
	return resp, nil
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
