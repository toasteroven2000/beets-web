package main

// Warning that a match would be a duplicate. beets only asks what to do about
// a duplicate after a match is chosen, so by then the page has said nothing.
// beets calls an Album a duplicate of another when they have the same album
// artist and album, and an Item when they have the same artist and title,
// compared exactly; the page asks the Library the same question up front.

import (
	"context"
	"strings"
)

// prompt names: what a prompt, or one of its Candidates, is called for the
// duplicate check.
func (p importPrompt) currentNames() (artist, name string) {
	if p.Kind == "item" {
		return p.Current["artist"], p.Current["title"]
	}
	return p.Current["artist"], p.Current["album"]
}

func (c candidate) duplicateName() string {
	if c.Album != "" {
		return c.Album
	}
	return c.Title
}

func knownKey(artist, name string) string { return artist + "\x00" + name }

// Duplicates are what the Library already has as this artist and name.
func (p importPrompt) Duplicates(artist, name string) []record {
	return p.Known[knownKey(artist, name)]
}

// duplicateScope is where the Library would hold a duplicate of the prompt.
func (p importPrompt) duplicateScope() scope {
	if p.Kind == "item" {
		return items
	}
	return albums
}

// duplicateLabel names a record the Library has.
func duplicateLabel(k scope, r record) string {
	if k == items {
		return "Item " + r.Str("id")
	}
	return "Album " + r.Str("id")
}

// markDuplicates finds what the Library already has under the prompt's own
// tags and each Candidate's names. A failure is ignored: the warning is a
// courtesy, and beets still asks about a duplicate when it comes to it.
func (s *server) markDuplicates(ctx context.Context, p *importPrompt) {
	if p.Kind != "album" && p.Kind != "item" {
		return
	}
	k := p.duplicateScope()
	first, second := "albumartist", "album"
	if k == items {
		first, second = "artist", "title"
	}
	type pair struct{ artist, name string }
	seen := map[pair]bool{}
	var groups []string
	add := func(artist, name string) {
		if artist == "" || name == "" || seen[pair{artist, name}] {
			return
		}
		seen[pair{artist, name}] = true
		groups = append(groups, shellQuote(first+":="+artist)+" "+shellQuote(second+":="+name))
	}
	add(p.currentNames())
	for _, c := range p.Candidates {
		add(c.Artist, c.duplicateName())
	}
	if len(groups) == 0 {
		return
	}
	res, err := s.api.List(ctx, k, strings.Join(groups, " , "), 100, 0, []string{"id", first, second})
	if err != nil {
		return
	}
	p.Known = map[string][]record{}
	for _, r := range res.Results {
		// The query is exact already; this keeps what beets would keep if it ever isn't.
		key := knownKey(r.Str(first), r.Str(second))
		if seen[pair{r.Str(first), r.Str(second)}] {
			p.Known[key] = append(p.Known[key], r)
		}
	}
}
