package main

// Cover thumbnails for the Library: each cover is downscaled once and kept in
// a cache directory, so a Library of covers doesn't fetch every full-size
// image from beets-api. A cached answer is trusted for thumbTTL after it was
// last checked, then checked again against beets-api's ETag. Art set or
// removed through beets-web drops the Album's entry straight away; art
// changed outside beets-web shows once the entry is next checked.

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
)

const (
	thumbSize = 320 // the longer side, in pixels
	thumbTTL  = time.Hour
)

// thumbMeta is what the cache knows about one Album's cover.
type thumbMeta struct {
	ETag    string    `json:"etag"` // beets-api's, for the full image
	NoArt   bool      `json:"no_art,omitempty"`
	Checked time.Time `json:"checked"`
}

type thumbCache struct {
	dir   string
	now   func() time.Time
	locks [64]sync.Mutex // by Album ID, so one cover is only made once at a time
}

func newThumbCache(dir string) (*thumbCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &thumbCache{dir: dir, now: time.Now}, nil
}

func (c *thumbCache) path(id int, ext string) string {
	return filepath.Join(c.dir, strconv.Itoa(id)+ext)
}

func (c *thumbCache) meta(id int) (thumbMeta, bool) {
	b, err := os.ReadFile(c.path(id, ".json"))
	if err != nil {
		return thumbMeta{}, false
	}
	var m thumbMeta
	if json.Unmarshal(b, &m) != nil {
		return thumbMeta{}, false
	}
	return m, true
}

func (c *thumbCache) save(id int, m thumbMeta, img []byte) error {
	if img != nil {
		if err := writeAtomic(c.path(id, ".jpg"), img); err != nil {
			return err
		}
	}
	b, _ := json.Marshal(m)
	return writeAtomic(c.path(id, ".json"), b)
}

// forget drops an Album's entry, so its next thumbnail is made afresh.
func (c *thumbCache) forget(id int) {
	if c == nil {
		return
	}
	l := &c.locks[id%len(c.locks)]
	l.Lock()
	defer l.Unlock()
	for _, ext := range []string{".json", ".jpg"} {
		if err := os.Remove(c.path(id, ext)); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("thumbnail cache: %v", err)
		}
	}
}

func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// thumbnail downscales an image to fit thumbSize, keeping its proportions,
// onto white so a transparent PNG doesn't turn black as a JPEG.
func thumbnail(data []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil, errors.New("empty image")
	}
	if w > thumbSize || h > thumbSize {
		if w >= h {
			w, h = thumbSize, max(1, h*thumbSize/b.Dx())
		} else {
			w, h = max(1, w*thumbSize/b.Dy()), thumbSize
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// thumbETag is the browser's ETag for a thumbnail, made from the full
// image's so it changes when the cover does.
func thumbETag(full string) string {
	return `"thumb-` + strings.Trim(strings.TrimPrefix(full, "W/"), `"`) + `"`
}

// handleThumb serves an Album's cover thumbnail. Without a cache directory it
// serves the full cover, as the Library did before thumbnails.
func (s *server) handleThumb(w http.ResponseWriter, r *http.Request) {
	c := s.thumbs
	if c == nil {
		s.handleArt(w, r)
		return
	}
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	l := &c.locks[id%len(c.locks)]
	l.Lock()
	m, ok := c.meta(id)
	if !ok || c.now().Sub(m.Checked) >= thumbTTL {
		m, ok = s.refreshThumb(r, id, m, ok)
	}
	l.Unlock()
	switch {
	case !ok:
		http.Redirect(w, r, artURL(id), http.StatusFound)
		return
	case m.NoArt:
		http.NotFound(w, r)
		return
	}
	etag := thumbETag(m.ETag)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", etag)
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	img, err := os.ReadFile(c.path(id, ".jpg"))
	if err != nil {
		c.forget(id)
		http.Redirect(w, r, artURL(id), http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(img)))
	w.Write(img)
}

// refreshThumb checks an Album's cover against beets-api and remakes its
// thumbnail if the cover changed. It returns false when there is nothing to
// serve from the cache; a cover that can't be made smaller is then served in
// full. If beets-api can't be asked, a cached thumbnail is kept as it was.
func (s *server) refreshThumb(r *http.Request, id int, old thumbMeta, cached bool) (thumbMeta, bool) {
	c := s.thumbs
	resp, err := s.api.AlbumArt(r.Context(), id, nil)
	if err != nil {
		if isStatus(err, http.StatusNotFound) {
			m := thumbMeta{NoArt: true, Checked: c.now()}
			c.saveNoArt(id, m)
			return m, true
		}
		return old, cached && !old.NoArt
	}
	defer resp.Body.Close()
	etag := resp.Header.Get("ETag")
	if cached && !old.NoArt && etag != "" && etag == old.ETag {
		old.Checked = c.now()
		if err := c.save(id, old, nil); err != nil {
			log.Printf("thumbnail cache: %v", err)
		}
		return old, true
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArtBytes+1))
	if err != nil {
		return old, cached && !old.NoArt
	}
	img, err := thumbnail(data)
	if err != nil {
		log.Printf("thumbnail for album %d: %v", id, err)
		return thumbMeta{}, false
	}
	m := thumbMeta{ETag: etag, Checked: c.now()}
	if etag == "" {
		m.ETag = strconv.FormatInt(c.now().UnixNano(), 36)
	}
	if err := c.save(id, m, img); err != nil {
		log.Printf("thumbnail cache: %v", err)
		return thumbMeta{}, false
	}
	return m, true
}

// saveNoArt records that an Album has no art, removing any old thumbnail.
func (c *thumbCache) saveNoArt(id int, m thumbMeta) {
	if err := os.Remove(c.path(id, ".jpg")); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("thumbnail cache: %v", err)
	}
	if err := c.save(id, m, nil); err != nil {
		log.Printf("thumbnail cache: %v", err)
	}
}

// etagMatches reports whether an If-None-Match header names etag.
func etagMatches(header, etag string) bool {
	if header == "" || etag == "" {
		return false
	}
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimPrefix(strings.TrimSpace(t), "W/")
		if t == "*" || t == etag {
			return true
		}
	}
	return false
}
