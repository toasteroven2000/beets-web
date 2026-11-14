package main

// What an Album page says about an Album: a one-line summary, its key facts,
// and its tracks by disc, with a row for each track the Album should have but
// the Library doesn't.

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// trackSlot is one place in a disc's track list: a track in the Library, or
// one the Album has but the Library doesn't.
type trackSlot struct {
	Number  int
	Row     record
	Missing bool
}

// Expected is how many tracks the disc should have, as its tracks say
// (`tracktotal`), or how many it has if they don't say.
func (d discTable) Expected() int {
	n := len(d.Rows)
	for _, r := range d.Rows {
		n = max(n, r.Int("tracktotal"))
	}
	return n
}

// Slots lists the disc's tracks in order, with a Missing slot wherever a
// numbered track is absent. Tracks with no number come last.
func (d discTable) Slots() []trackSlot {
	var slots, unnumbered []trackSlot
	next, expected := 1, d.Expected()
	for _, r := range d.Rows {
		t := r.Int("track")
		if t == 0 {
			unnumbered = append(unnumbered, trackSlot{Row: r})
			continue
		}
		for ; next < t && next <= expected; next++ {
			slots = append(slots, trackSlot{Number: next, Missing: true})
		}
		slots = append(slots, trackSlot{Number: t, Row: r})
		next = max(next, t+1)
	}
	for ; next <= expected && len(slots)+len(unnumbered) < expected; next++ {
		slots = append(slots, trackSlot{Number: next, Missing: true})
	}
	return append(slots, unnumbered...)
}

// Format is a track's file type and bitrate, as "MP3 · 32 kbps".
func trackFormat(r record) string {
	s := r.Str("format")
	if kbps := r.Int("bitrate") / 1000; kbps > 0 {
		if s != "" {
			s += " · "
		}
		s += strconv.Itoa(kbps) + " kbps"
	}
	return s
}

func isExplicit(r record) bool { return strings.EqualFold(r.Str("trackdisambig"), "explicit") }

// artistsDiffer tells whether any track is by someone other than the Album's
// artist, as on a compilation, so the track list needs an Artist column.
func artistsDiffer(album record, rows []record) bool {
	for _, r := range rows {
		if a := r.Str("artist"); a != "" && a != album.Str("albumartist") {
			return true
		}
	}
	return false
}

type factGroup struct {
	Title string
	Rows  [][2]string
}

// albumInfo is what the Album page's sidebar and details say.
type albumInfo struct {
	Summary    string
	Facts      [][2]string
	Groups     []factGroup
	Source     string // where the Album's metadata came from, with its page if known
	SourceURL  string
	ShowArtist bool
}

// recordDate is a record's year, month and day as one date, leaving out zero
// parts. prefix picks the original release: "original_".
func recordDate(r record, prefix string) string {
	var parts []string
	for i, k := range []string{"year", "month", "day"} {
		n := r.Int(prefix + k)
		if n == 0 {
			break
		}
		if i == 0 {
			parts = append(parts, strconv.Itoa(n))
		} else {
			parts = append(parts, fmt.Sprintf("%02d", n))
		}
	}
	return strings.Join(parts, "-")
}

func timeText(seconds float64) string {
	s := int(math.Round(seconds))
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s%3600/60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func present(rows [][2]string) [][2]string {
	var out [][2]string
	for _, r := range rows {
		if r[1] != "" {
			out = append(out, r)
		}
	}
	return out
}

func albumInfoOf(rec record, rows []record, discs []discTable) albumInfo {
	released, original := recordDate(rec, ""), recordDate(rec, "original_")
	if original == released {
		original = ""
	}
	added := ""
	if f, ok := rec["added"]; ok {
		if secs, err := strconv.ParseFloat(valueString(f), 64); err == nil && secs > 0 {
			added = time.Unix(int64(secs), 0).UTC().Format("2006-01-02")
		}
	}
	info := albumInfo{
		Facts: present([][2]string{{"Released", released}, {"Original release", original}, {"Label", rec.Str("label")},
			{"Catalogue no.", rec.Str("catalognum")}, {"Country", rec.Str("country")}, {"Media", rec.Str("media")},
			{"Type", rec.Str("albumtype")}, {"Status", rec.Str("albumstatus")}, {"Barcode", rec.Str("barcode")}, {"Genres", rec.Str("genres")}}),
		Source:     rec.Str("data_source"),
		ShowArtist: artistsDiffer(rec, rows),
	}
	for _, g := range []factGroup{
		{"Release", [][2]string{{"Album", rec.Str("album")}, {"Album artist", rec.Str("albumartist")}, {"Released", released},
			{"Original release", original}, {"Type", rec.Str("albumtype")}, {"Status", rec.Str("albumstatus")}, {"Disambiguation", rec.Str("albumdisambig")}}},
		{"Label", [][2]string{{"Label", rec.Str("label")}, {"Catalogue no.", rec.Str("catalognum")}, {"Barcode", rec.Str("barcode")},
			{"Country", rec.Str("country")}, {"Media", rec.Str("media")}}},
		{"Text", [][2]string{{"Language", rec.Str("language")}, {"Script", rec.Str("script")}, {"Genres", rec.Str("genres")}}},
		{"Source", [][2]string{{"Source", rec.Str("data_source")}, {"MusicBrainz release", rec.Str("mb_albumid")}, {"Added", added}}},
	} {
		if g.Rows = present(g.Rows); len(g.Rows) > 0 {
			info.Groups = append(info.Groups, g)
		}
	}
	if id := rec.Str("mb_albumid"); id != "" && strings.EqualFold(info.Source, "musicbrainz") {
		info.SourceURL = "https://musicbrainz.org/release/" + id
	}

	// The summary: year, tracks (of how many there should be), discs, length, format.
	expected := 0
	for _, d := range discs {
		expected += d.Expected()
	}
	tracks := plural(len(rows), "track", "tracks")
	if expected > len(rows) {
		tracks = fmt.Sprintf("%d of %d tracks", len(rows), expected)
	}
	var length float64
	var formats []string
	for _, r := range rows {
		if f, err := strconv.ParseFloat(r.Str("length"), 64); err == nil {
			length += f
		}
		if f := r.Str("format"); f != "" && !slices.Contains(formats, f) {
			formats = append(formats, f)
		}
	}
	parts := []string{yearText(rec), tracks, plural(albumDiscCount(rec, discs), "disc", "discs")}
	if length > 0 {
		parts = append(parts, timeText(length))
	}
	if len(formats) > 0 {
		parts = append(parts, strings.Join(formats, ", "))
	}
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	info.Summary = strings.Join(kept, " · ")
	return info
}
