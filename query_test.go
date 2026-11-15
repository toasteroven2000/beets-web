package main

import (
	"reflect"
	"regexp"
	"testing"
)

func TestArtistPatternMatchesWholeNames(t *testing.T) {
	seps := defaultSeparators
	cases := []struct {
		name, credit string
		want         bool
	}{
		{"Queen", "Queen", true},
		{"Beyoncé", "Jay-Z & Beyoncé", true},
		{"Jay-Z", "Jay-Z & Beyoncé", true},
		{"Jay-Z", "Jay-Z&Beyoncé", true},
		{"B", "A, B; C", true},
		{"Prince", "Princess Nokia", false},
		{"Prince", "The Prince", false},
		{"98°", "98° and Stevie Wonder", false},
		{"98° and Stevie Wonder", "98° and Stevie Wonder", true},
		{"*NSYNC", "*NSYNC", true},
		{"AC/DC", "AC/DC", true},
		{"a.b", "axb", false},
	}
	for _, c := range cases {
		re := regexp.MustCompile(artistPattern(c.name, seps))
		if got := re.MatchString(c.credit); got != c.want {
			t.Errorf("%q in %q: got %v, want %v", c.name, c.credit, got, c.want)
		}
	}
}

func TestArtistQueryQuotesForShlex(t *testing.T) {
	got := artistQuery(albums, "Guns N' Roses", []string{"&"})
	want := `'albumartist::^(?:.*(?:&))?\s*Guns N'"'"' Roses\s*(?:(?:&).*)?$'`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := artistQuery(items, "X", nil); got != `'artist::^\s*X\s*$'` {
		t.Errorf("no separators: %s", got)
	}
}

func TestSplitArtists(t *testing.T) {
	got := splitArtists(" Jay-Z & Beyoncé, Kanye ;", defaultSeparators)
	want := []string{"Jay-Z", "Beyoncé", "Kanye"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
}

func TestFiltersCombined(t *testing.T) {
	f := filters{Query: " love ", YearFrom: "1960", Genre: "Rock & Roll", Format: "FLAC"}
	want := `love year:1960.. 'genres:Rock & Roll' 'format:FLAC'`
	if got := f.Combined(albums, nil); got != want {
		t.Errorf("got %s", got)
	}
}

func TestSortQuery(t *testing.T) {
	cases := map[string]string{
		sortQuery("", albums, "artist"):           "albumartist+ year+ album+",
		sortQuery("x", items, "recent"):           "x added-",
		sortQuery("x year-", albums, "artist"):    "x year-",
		sortQuery("year:2000..", items, "artist"): "year:2000.. artist+ album+ disc+ track+",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestIDsQuery(t *testing.T) {
	if got := idsQuery([]int{3, 7}); got != "id:3 , id:7" {
		t.Errorf("got %q", got)
	}
}

func TestModRequestScope(t *testing.T) {
	if s := (modRequest{Scope: "album"}).scope(); s != albums {
		t.Errorf("scope album: %s", s)
	}
	if s := (modRequest{}).scope(); s != items {
		t.Errorf("default: %s", s)
	}
}
