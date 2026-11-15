package main

// Where an Import is: which Task it is on, how many beets expects in all, what
// has been done and what is to come. beets-api knows the total only as an
// estimate, found by walking the Import Paths when the Import starts.

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	remainingPerPage = 100 // Tasks the Remaining filter lists at once
	logPendingRows   = 100 // upcoming Tasks the whole Log shows before "more": every one, unless there are hundreds
)

// importPage is the Import page: the Import itself, and the tab it is on.
type importPage struct {
	Op           importOp
	Stale        bool
	NoSources    bool
	Mode         importMode
	Tab          string // "now", "log" or "paths"
	Show         string // which Tasks the Log lists: "all", "applied", "asis", "skipped" or "remaining"
	Offset       int    // into the remaining Tasks
	Pending      []importTask
	PendingTotal int
}

func importTab(s string) string {
	switch s {
	case "log", "paths":
		return s
	}
	return "now"
}

func importShow(s string) string {
	switch s {
	case "applied", "asis", "skipped", "remaining":
		return s
	}
	return "all"
}

// TabURL is the page on another tab.
func (p importPage) TabURL(tab string) string {
	u := "/imports/" + string(p.Op.ID)
	if tab != "now" {
		u += "?tab=" + tab
	}
	return u
}

// LogURL is the Log, showing some of its Tasks.
func (p importPage) LogURL(show string, offset int) string {
	v := url.Values{"tab": {"log"}}
	if show != "all" {
		v.Set("show", show)
	}
	if offset > 0 {
		v.Set("offset", strconv.Itoa(offset))
	}
	return "/imports/" + string(p.Op.ID) + "?" + v.Encode()
}

// PollURL is the page as it is now, for refreshing it while beets works.
func (p importPage) PollURL() string {
	if p.Tab == "log" {
		return p.LogURL(p.Show, p.Offset)
	}
	return p.TabURL(p.Tab)
}

// Folder is a path as it reads under the Import Path it came from.
func (p importPage) Folder(path string) string { return relativeTo(p.Op.Request.Paths, path) }

func relativeTo(roots []string, path string) string {
	for _, r := range roots {
		r = strings.TrimRight(r, "/")
		if rel, ok := strings.CutPrefix(path, r+"/"); ok {
			return rel
		}
	}
	return path
}

func (p importPage) folderOf(paths []string) string {
	rel := make([]string, len(paths))
	for i, q := range paths {
		rel[i] = p.Folder(q)
	}
	return strings.Join(rel, ", ")
}

// UpNext is the folder beets will ask about after the one it is on.
func (p importPage) UpNext() string {
	for _, t := range p.Pending {
		if t.State == "pending" {
			return p.folderOf(t.Paths)
		}
	}
	return ""
}

// logRow is one finished Task of the Log.
type logRow struct {
	Number int
	Folder string
	Result string
	Kind   string // applied, asis, skipped or other
	By     string
}

var logResults = map[string][2]string{
	"apply":  {"Applied", "applied"},
	"asis":   {"Used as-is", "asis"},
	"skip":   {"Skipped", "skipped"},
	"tracks": {"Imported as tracks", "other"},
	"albums": {"Grouped into Albums", "other"},
	"keep":   {"Kept both", "other"},
	"remove": {"Removed old", "other"},
	"merge":  {"Merged", "other"},
	"abort":  {"Aborted", "other"},
}

// Done are the Tasks answered so far, oldest first, each by its last Choice
// (a search leads to another prompt for the same Task). The Task beets is
// asking about now is not done.
func (p importPage) Done() []logRow {
	var current string
	if pr := p.Op.Result.Prompt; pr != nil {
		current = strings.Join(pr.Paths, "\x00")
	}
	last := map[string]importChoice{}
	var order []string
	for _, c := range p.Op.Result.Choices {
		key := strings.Join(c.Paths, "\x00")
		if _, seen := last[key]; !seen {
			order = append(order, key)
		}
		if c.Action != "search" || last[key].Action == "" {
			last[key] = c
		}
	}
	var rows []logRow
	for _, key := range order {
		c := last[key]
		if key == current || c.Action == "search" {
			continue
		}
		res, kind := logResults[c.Action][0], logResults[c.Action][1]
		if res == "" {
			res, kind = choiceLabel(c.Action), "other"
		}
		by := ""
		if c.Actor != nil {
			by = *c.Actor
		}
		rows = append(rows, logRow{Number: len(rows) + 1, Folder: p.folderOf(c.Paths), Result: res, Kind: kind, By: by})
	}
	return rows
}

// Newest is the done Tasks of the Log, newest first, for the filter chosen.
func (p importPage) Newest(show string) []logRow {
	var rows []logRow
	for _, r := range p.Done() {
		if show == "all" || show == r.Kind {
			rows = append([]logRow{r}, rows...)
		}
	}
	return rows
}

func (p importPage) Count(kind string) int {
	n := 0
	for _, r := range p.Done() {
		if r.Kind == kind {
			n++
		}
	}
	return n
}

// Total is how many Tasks beets expects, or 0 if it doesn't know.
func (p importPage) Total() int {
	if p.Op.Result.ExpectedTasks == nil {
		return 0
	}
	return *p.Op.Result.ExpectedTasks
}

// Remaining is how many Tasks are still to come after the current one.
func (p importPage) Remaining() int {
	if p.Total() == 0 || !importActive(p.Op.Status) {
		return 0
	}
	n := p.Total() - p.Op.Result.TasksDone
	if p.Op.Status == "waiting_for_choice" {
		n--
	}
	return max(n, 0)
}

// Step says which Task the Import is on.
func (p importPage) Step() string {
	done := p.Op.Result.TasksDone
	switch p.Op.Status {
	case "completed":
		return "Finished: " + plural(done, "Task", "Tasks")
	case "aborted", "failed", "interrupted":
		return plural(done, "Task", "Tasks") + " done"
	}
	n := done + 1
	s := "Task " + strconv.Itoa(n)
	switch {
	case p.Total() > 0 && n > p.Total():
		s += fmt.Sprintf(" (%d were expected)", p.Total())
	case p.Total() > 0:
		s += " of " + strconv.Itoa(p.Total())
	}
	return s
}

// Percent is how far through the expected Tasks the Import is.
func (p importPage) Percent() int {
	if p.Total() == 0 {
		return 0
	}
	n := p.Op.Result.TasksDone
	if p.Op.Status == "waiting_for_choice" || p.Op.Status == "running" {
		n++
	}
	return min(n*100/p.Total(), 100)
}

// Segment is how wide one part of the progress bar is, as a style.
func (p importPage) Segment(kind string) string {
	if p.Total() == 0 {
		return "width: 0%"
	}
	n := p.Count(kind)
	if kind == "now" {
		n = 0
		if importActive(p.Op.Status) {
			n = 1
		}
	}
	return fmt.Sprintf("width: %.2f%%", min(float64(n)*100/float64(p.Total()), 100))
}

// OtherDone counts finished Tasks that were neither applied, as-is nor skipped.
func (p importPage) OtherDone() int { return p.Count("other") }

// firstPending is the number of the first Task still to come.
func (p importPage) firstPending(done int) int {
	if p.Show == "remaining" {
		return done + p.Offset + 2 // after the current Task
	}
	if p.Op.Status == "waiting_for_choice" {
		return done + 2
	}
	return done + 1
}
