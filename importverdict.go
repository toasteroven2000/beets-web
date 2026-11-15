package main

// What applying a Candidate would do, said the way a person decides: first
// what could go wrong (a duplicate, files left out, values overwritten), then
// what is simply done (tracks renamed, empty fields filled in), then what is
// safe. Filling in an empty field is not the same as replacing one you have,
// so the two are never counted together.

import (
	"strconv"
	"strings"
)

// verdict is what applying one Candidate would do.
type verdict struct {
	Album      bool     // an Album prompt, not an Item's
	Duplicates []record // what the Library already has under the Candidate's names
	Files      int      // your files in the Task
	Unmatched  []string // your files that match no track of the release
	Missing    []string // tracks of the release no file matches
	Overwrites []factRow
	Fills      []factRow
	Renames    []trackRow
	Renumbers  int
}

func verdictOf(p importPrompt, c candidate) verdict {
	v := verdict{Album: c.Track == nil, Duplicates: p.Duplicates(c.Artist, c.duplicateName())}
	for _, f := range candidateFacts(c) {
		switch {
		case !f.Changed:
		case f.Old == "":
			v.Fills = append(v.Fills, f)
		default:
			v.Overwrites = append(v.Overwrites, f)
		}
	}
	if !v.Album {
		v.Files = 1
		return v
	}
	v.Files = len(candidateFiles(c)) + len(c.ExtraItems)
	for _, path := range c.ExtraItems {
		v.Unmatched = append(v.Unmatched, baseName(path))
	}
	for _, t := range c.ExtraTracks {
		v.Missing = append(v.Missing, valueString(t))
	}
	for _, r := range candidateTracks(c) {
		if r.TitleChanged && r.OldTitle != "" {
			v.Renames = append(v.Renames, r)
		}
		if r.NumChanged && r.OldNum != "" {
			v.Renumbers++
		}
	}
	return v
}

// Tracks is how many tracks the Candidate matched to your files.
func (v verdict) Tracks() int { return v.Files - len(v.Unmatched) }

// AllMatch tells whether every file matches a track and every track a file.
func (v verdict) AllMatch() bool {
	return v.Album && v.Files > 0 && len(v.Unmatched) == 0 && len(v.Missing) == 0
}

// Keeps says what applying leaves alone, when that is worth saying: nothing
// overwritten, or nothing besides the renamed titles.
func (v verdict) Keeps() string {
	if len(v.Overwrites) > 0 || len(v.Unmatched) > 0 {
		return ""
	}
	if len(v.Renames) > 0 {
		return "No other field you have is overwritten."
	}
	return "Nothing you have is overwritten."
}

// OverwritesText is the overwritten values in a few words, for the table.
func (v verdict) OverwritesText() string {
	var parts []string
	for _, f := range v.Overwrites {
		parts = append(parts, strings.ToLower(f.Label)+" "+f.Old+" → "+f.New)
	}
	return strings.Join(parts, ", ")
}

// candidateFact is one fact of a Candidate as it would be once applied.
func candidateFact(c candidate, label string) string {
	for _, f := range candidateFacts(c) {
		if f.Label == label {
			return f.New
		}
	}
	return ""
}

// candidateSummary is the year, label, country and media a release is told
// apart by.
func candidateSummary(c candidate) string {
	var parts []string
	if y := valueString(c.Year); y != "" && y != "0" {
		parts = append(parts, y)
	}
	for _, l := range []string{"Label", "Country", "Media"} {
		if v := candidateFact(c, l); v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " · ")
}

// applyFormID is the form a Candidate's Apply button submits.
func applyFormID(c candidate) string { return "apply-" + strconv.Itoa(c.Index) }
