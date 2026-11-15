package main

// The Library: one page for browsing and searching Albums and Items, in place
// of separate Albums, Items and Search pages. A tab picks Albums or Items, each
// counted for the current search; with a search an All tab shows both. Acting
// on everything shown (Modify all, Remove all) is only offered once a search or
// filter has narrowed it, never for the whole Library.

import (
	"net/http"
	"net/url"
	"strings"
)

// librarySorts are the orders the Library can be listed in, by name.
var librarySorts = []struct{ Name, Label string }{
	{"artist", "Artist"},
	{"recent", "Recently added"},
	{"title", "Title"},
	{"year", "Year"},
}

// sortQuery adds beets sort terms for an order to a query, unless the query
// sorts itself.
func sortQuery(query string, k scope, sort string) string {
	if hasSort(query) {
		return query
	}
	var s string
	switch {
	case sort == "recent":
		s = "added-"
	case sort == "title" && k == albums:
		s = "album+ albumartist+"
	case sort == "title":
		s = "title+ artist+"
	case sort == "year" && k == albums:
		s = "year- albumartist+ album+"
	case sort == "year":
		s = "year- artist+ album+ disc+ track+"
	case k == albums:
		s = "albumartist+ year+ album+"
	default:
		s = "artist+ album+ disc+ track+"
	}
	return strings.TrimSpace(query + " " + s)
}

func librarySort(s string) string {
	for _, o := range librarySorts {
		if o.Name == s {
			return s
		}
	}
	return "artist"
}

// libraryPage is the Library as asked for.
type libraryPage struct {
	Show    string // "all", "albums" or "items"
	View    string // Albums as "grid" or "list"
	Sort    string
	Filters filters
	Separators []string
	Counts  map[scope]int
	Albums  tableData
	Items   tableData
	Error   string
}

// Scoped tells whether a search or filter narrows the Library. Only then is
// acting on everything shown offered: the blast radius is never everything.
func (p libraryPage) Scoped() bool { return p.Filters.Combined(albums, nil) != "" }

// Query is what the Library searches k for.
func (p libraryPage) Query(k scope) string { return p.Filters.Combined(k, p.Separators) }

// URL is the Library with some of its settings changed.
func (p libraryPage) URL(change func(*libraryPage)) string {
	q := p
	if change != nil {
		change(&q)
	}
	v := url.Values{}
	set := func(k, val string) {
		if val != "" {
			v.Set(k, val)
		}
	}
	if q.Show != "albums" || q.Scoped() {
		set("show", q.Show)
	}
	set("q", q.Filters.Query)
	set("artist", q.Filters.Artist)
	set("year_from", q.Filters.YearFrom)
	set("year_to", q.Filters.YearTo)
	set("genre", q.Filters.Genre)
	set("albumtype", q.Filters.AlbumType)
	set("format", q.Filters.Format)
	if q.Sort != "artist" {
		set("sort", q.Sort)
	}
	if q.View == "list" {
		set("view", "list")
	}
	if len(v) == 0 {
		return "/library"
	}
	return "/library?" + v.Encode()
}

func (p libraryPage) ShowURL(show string) string {
	return p.URL(func(q *libraryPage) { q.Show = show })
}

func (p libraryPage) SortURL(sort string) string {
	return p.URL(func(q *libraryPage) { q.Sort = sort })
}

func (p libraryPage) ViewURL(view string) string {
	return p.URL(func(q *libraryPage) { q.View = view })
}

func (p libraryPage) SortLabel() string {
	for _, o := range librarySorts {
		if o.Name == p.Sort {
			return o.Label
		}
	}
	return ""
}

// filterChip is one filter in use, and the Library without it.
type filterChip struct{ Label, Without string }

func (p libraryPage) Chips() []filterChip {
	f := p.Filters
	var chips []filterChip
	add := func(label string, clear func(*filters)) {
		chips = append(chips, filterChip{label, p.URL(func(q *libraryPage) { clear(&q.Filters) })})
	}
	if f.Artist != "" {
		add("artist: "+f.Artist, func(f *filters) { f.Artist = "" })
	}
	if f.YearFrom != "" || f.YearTo != "" {
		add("year "+orDash(f.YearFrom)+"–"+orDash(f.YearTo), func(f *filters) { f.YearFrom, f.YearTo = "", "" })
	}
	if f.Genre != "" {
		add("genre: "+f.Genre, func(f *filters) { f.Genre = "" })
	}
	if f.AlbumType != "" {
		add("album type: "+f.AlbumType, func(f *filters) { f.AlbumType = "" })
	}
	if f.Format != "" {
		add("format: "+f.Format, func(f *filters) { f.Format = "" })
	}
	return chips
}

// ClearFiltersURL is the Library with the search kept and the filters gone.
func (p libraryPage) ClearFiltersURL() string {
	return p.URL(func(q *libraryPage) { q.Filters = filters{Query: q.Filters.Query} })
}

// libraryURL is the Library showing Albums or Items.
func libraryURL(k scope) string {
	if k == items {
		return "/library?show=items"
	}
	return "/library"
}

// The All tab shows a few of each; the Albums and Items tabs list pageSize at
// a time, and more load as you scroll.
const (
	allAlbums = 12
	allItems  = 10
)

func (s *server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p := libraryPage{Filters: filtersFrom(r), Sort: librarySort(q.Get("sort")), View: "grid", Show: q.Get("show"), Counts: map[scope]int{}}
	if q.Get("view") == "list" {
		p.View = "list"
	}
	switch {
	case p.Show == "items" || p.Show == "albums":
	case p.Show == "all" && p.Scoped(), p.Show == "" && p.Scoped():
		p.Show = "all"
	default:
		p.Show = "albums"
	}
	ctx := r.Context()
	seps := s.separators(ctx)
	p.Separators = seps
	list := func(k scope, limit, offset int) (tableData, error) {
		res, err := s.api.List(ctx, k, sortQuery(p.Query(k), k, p.Sort), limit, offset, listFields[k])
		if err != nil {
			return tableData{}, err
		}
		p.Counts[k] = res.Total
		return tableData{Scope: k, Query: p.Query(k), Rows: res.Results, Total: res.Total, Next: nextURL(r, res), Separators: seps}, nil
	}
	count := func(k scope) error {
		res, err := s.api.List(ctx, k, p.Query(k), 1, 0, []string{"id"})
		p.Counts[k] = res.Total
		return err
	}
	var err error
	switch p.Show {
	case "all":
		if p.Albums, err = list(albums, allAlbums, 0); err == nil {
			p.Items, err = list(items, allItems, 0)
		}
		p.Albums.Next, p.Items.Next = "", ""
	case "items":
		if p.Items, err = list(items, pageSize, offsetParam(r)); err == nil && !isMore(r) {
			err = count(albums)
		}
	default:
		if p.Albums, err = list(albums, pageSize, offsetParam(r)); err == nil && !isMore(r) {
			err = count(items)
		}
	}
	if err != nil {
		if isStatus(err, http.StatusBadRequest) && !isMore(r) {
			p.Error = errorMessage(err)
			render(w, http.StatusBadRequest, libraryView(p))
			return
		}
		fail(w, err)
		return
	}
	if isMore(r) {
		render(w, http.StatusOK, libraryMore(p))
		return
	}
	render(w, http.StatusOK, libraryView(p))
}

// redirectToLibrary sends the old Albums, Items and Search pages to the
// Library, keeping what they were asked.
func redirectToLibrary(show string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if show != "" {
			q.Set("show", show)
		}
		if q.Get("order") == "recent" {
			q.Del("order")
			q.Set("sort", "recent")
		}
		u := "/library"
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		http.Redirect(w, r, u, http.StatusFound)
	}
}

// modifyAllURL Modifies everything a query matches.
func modifyAllURL(k scope, query string) string {
	return "/modify?" + url.Values{"scope": {string(k)}, "q": {query}}.Encode()
}
