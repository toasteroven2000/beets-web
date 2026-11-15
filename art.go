package main

// Album art: finding candidates on iTunes and Deezer, fetching an image from
// a URL, and holding a fetched image while its replacement is compared.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	userAgent   = "beets-web (album art search)"
	maxArtBytes = 20 << 20 // beets-api's limit
)

// artCandidate is one image a source offers for an Album.
type artCandidate struct {
	Thumb, Full, Page, Label, Source string
	Artist, Title                    string
	Tracks, Year                     int // 0 when the source doesn't say
	Width, Height                    int // 0 when the header couldn't be read
	rank                             int // position in its source's results

	// How it matched the Album, set by rankArt.
	Score                  int
	TracksMatch, YearMatch bool
}

func (c artCandidate) Dims() string { return dimsText(c.Width, c.Height) }

func dimsText(w, h int) string {
	if w == 0 || h == 0 {
		return "size unknown"
	}
	return strconv.Itoa(w) + "×" + strconv.Itoa(h)
}

// artSearch searches the art sources. The base URLs are fields so tests can
// point them at a fake.
type artSearch struct {
	http   *http.Client
	itunes string
	deezer string
}

func newArtSearch() *artSearch {
	return &artSearch{
		http:   &http.Client{Timeout: 15 * time.Second},
		itunes: "https://itunes.apple.com/search",
		deezer: "https://api.deezer.com/search/album",
	}
}

// Search asks every source at once. A source that fails or finds nothing is
// reported in problems; the others still count. Candidates whose image can't
// be reached are dropped; rankArt orders the rest.
func (a *artSearch) Search(ctx context.Context, query string) (found []artCandidate, problems []string) {
	sources := []struct {
		name   string
		search func(context.Context, string) ([]artCandidate, error)
	}{{"iTunes", a.searchITunes}, {"Deezer", a.searchDeezer}}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, src := range sources {
		wg.Go(func() {
			c, err := src.search(ctx, query)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				problems = append(problems, src.name+": "+err.Error())
			case len(c) == 0:
				problems = append(problems, src.name+": no results")
			}
			found = append(found, c...)
		})
	}
	wg.Wait()
	slices.Sort(problems)

	ok := make([]bool, len(found))
	for i := range found {
		wg.Go(func() {
			found[i].Width, found[i].Height, ok[i] = a.probe(ctx, found[i].Full)
		})
	}
	wg.Wait()
	var live []artCandidate
	for i, c := range found {
		if ok[i] {
			live = append(live, c)
		}
	}
	return live, problems
}

// artTarget is what beets knows about the Album the art is for.
type artTarget struct {
	Artist, Title string
	Tracks, Year  int
}

// rankArt orders candidates by how well they match the Album: artist and
// title (ignoring case, punctuation and bracketed suffixes like
// "(Remastered)"), then track count and year. Ties go to the larger image,
// then to the sources' own order.
func rankArt(found []artCandidate, t artTarget) {
	for i := range found {
		c := &found[i]
		c.Score = nameScore(c.Artist, t.Artist) + nameScore(c.Title, t.Title)
		c.TracksMatch = c.Tracks != 0 && c.Tracks == t.Tracks
		c.YearMatch = c.Year != 0 && c.Year == t.Year
		if c.TracksMatch {
			c.Score += 2
		}
		if c.YearMatch {
			c.Score++
		}
	}
	slices.SortStableFunc(found, func(x, y artCandidate) int {
		if x.Score != y.Score {
			return y.Score - x.Score
		}
		if ax, ay := x.Width*x.Height, y.Width*y.Height; ax != ay {
			return ay - ax
		}
		return x.rank - y.rank
	})
}

// nameScore is 4 for the same name, 2 when one contains the other, else 0.
func nameScore(got, want string) int {
	g, w := normName(got), normName(want)
	switch {
	case g == "" || w == "":
		return 0
	case g == w:
		return 4
	case strings.Contains(g, w) || strings.Contains(w, g):
		return 2
	}
	return 0
}

// normName drops bracketed parts, "&"/"and" differences, punctuation and case.
func normName(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range strings.ToLower(strings.ReplaceAll(s, "&", " and ")) {
		switch {
		case r == '(' || r == '[':
			depth++
		case r == ')' || r == ']':
			if depth > 0 {
				depth--
			}
		case depth > 0:
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (a *artSearch) getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// searchITunes asks for 3000px art; iTunes serves up to that, or the original
// if it's smaller.
func (a *artSearch) searchITunes(ctx context.Context, query string) ([]artCandidate, error) {
	var body struct {
		Results []struct {
			ArtistName        string `json:"artistName"`
			CollectionName    string `json:"collectionName"`
			ArtworkURL100     string `json:"artworkUrl100"`
			CollectionViewURL string `json:"collectionViewUrl"`
			TrackCount        int    `json:"trackCount"`
			ReleaseDate       string `json:"releaseDate"`
		} `json:"results"`
	}
	v := url.Values{"media": {"music"}, "entity": {"album"}, "limit": {"12"}, "term": {query}}
	if err := a.getJSON(ctx, a.itunes+"?"+v.Encode(), &body); err != nil {
		return nil, err
	}
	var out []artCandidate
	for i, r := range body.Results {
		if !strings.Contains(r.ArtworkURL100, "100x100bb") {
			continue
		}
		year := 0
		if len(r.ReleaseDate) >= 4 {
			year, _ = strconv.Atoi(r.ReleaseDate[:4])
		}
		out = append(out, artCandidate{
			Thumb:  strings.Replace(r.ArtworkURL100, "100x100bb", "300x300bb", 1),
			Full:   strings.Replace(r.ArtworkURL100, "100x100bb", "3000x3000bb", 1),
			Page:   r.CollectionViewURL,
			Label:  r.ArtistName + " — " + r.CollectionName,
			Source: "iTunes",
			Artist: r.ArtistName,
			Title:  r.CollectionName,
			Tracks: r.TrackCount,
			Year:   year,
			rank:   i,
		})
	}
	return out, nil
}

// searchDeezer uses cover_xl, Deezer's largest (1000px).
func (a *artSearch) searchDeezer(ctx context.Context, query string) ([]artCandidate, error) {
	var body struct {
		Data []struct {
			Title       string `json:"title"`
			Link        string `json:"link"`
			CoverXL     string `json:"cover_xl"`
			CoverMedium string `json:"cover_medium"`
			NbTracks    int    `json:"nb_tracks"`
			Artist      struct {
				Name string `json:"name"`
			} `json:"artist"`
		} `json:"data"`
	}
	v := url.Values{"limit": {"12"}, "q": {query}}
	if err := a.getJSON(ctx, a.deezer+"?"+v.Encode(), &body); err != nil {
		return nil, err
	}
	var out []artCandidate
	for i, r := range body.Data {
		if r.CoverXL == "" {
			continue
		}
		thumb := r.CoverMedium
		if thumb == "" {
			thumb = r.CoverXL
		}
		out = append(out, artCandidate{
			Thumb:  thumb,
			Full:   r.CoverXL,
			Page:   r.Link,
			Label:  r.Artist.Name + " — " + r.Title,
			Source: "Deezer",
			Artist: r.Artist.Name,
			Title:  r.Title,
			Tracks: r.NbTracks,
			rank:   i,
		})
	}
	return out, nil
}

// probe reads the start of an image for its real size. ok reports whether the
// image exists; the size may still be unknown if the header is further in.
func (a *artSearch) probe(ctx context.Context, u string) (w, h int, ok bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, 0, false
	}
	req.Header.Set("Range", "bytes=0-65535")
	req.Header.Set("User-Agent", userAgent)
	resp, err := a.http.Do(req)
	if err != nil {
		return 0, 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return 0, 0, false
	}
	w, h = imageSize(io.LimitReader(resp.Body, 65536))
	return w, h, true
}

func imageSize(r io.Reader) (w, h int) {
	cfg, _, err := image.DecodeConfig(r)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// Fetch downloads an image to Set art from, which must be a JPEG or PNG.
func (a *artSearch) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("enter an http or https URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't fetch the image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("couldn't fetch the image: %s", resp.Status)
	}
	return readImage(resp.Body)
}

// readImage reads at most beets-api's limit and checks the format from the
// bytes, as beets-api does.
func readImage(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxArtBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArtBytes {
		return nil, fmt.Errorf("the image is larger than %d MiB", maxArtBytes>>20)
	}
	if _, ok := imageType(data); !ok {
		return nil, fmt.Errorf("the image must be a JPEG or PNG, not %s", http.DetectContentType(data))
	}
	return data, nil
}

func imageType(data []byte) (string, bool) {
	switch t := http.DetectContentType(data); t {
	case "image/jpeg", "image/png":
		return t, true
	}
	return "", false
}

// pendingArt holds images fetched to replace an Album's art while the two are
// compared, so the image you confirm is the one you saw.
type pendingArt struct {
	mu sync.Mutex
	m  map[string]pendingImage
}

type pendingImage struct {
	album   int
	data    []byte
	source  string // the URL or file name it came from
	created time.Time
}

const pendingTTL = time.Hour

func (p *pendingArt) put(album int, data []byte, source string) string {
	b := make([]byte, 16)
	rand.Read(b)
	token := hex.EncodeToString(b)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.m == nil {
		p.m = map[string]pendingImage{}
	}
	for k, v := range p.m {
		if time.Since(v.created) > pendingTTL {
			delete(p.m, k)
		}
	}
	p.m[token] = pendingImage{album: album, data: data, source: source, created: time.Now()}
	return token
}

func (p *pendingArt) get(album int, token string) (pendingImage, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.m[token]
	if !ok || v.album != album || time.Since(v.created) > pendingTTL {
		return pendingImage{}, false
	}
	return v, true
}

func (p *pendingArt) remove(token string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.m, token)
}

func (v pendingImage) size() (w, h int) { return imageSize(bytes.NewReader(v.data)) }
