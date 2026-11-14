package main

// How an Audit entry reads to a person: a one-line summary, an icon and colour
// for its action, and its changes laid out by what kind of value they are.

import (
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

// auditVerb is the last part of an action: modify, import, remove, upload.
func auditVerb(action string) string {
	_, verb, _ := strings.Cut(action, ".")
	return verb
}

// auditTone is the Bootstrap colour for an action.
func auditTone(action string) string {
	switch {
	case action == "art.remove" || action == "trash.purge":
		return "danger"
	case auditVerb(action) == "modify":
		return "primary"
	case auditVerb(action) == "import":
		return "success"
	case auditVerb(action) == "remove":
		return "danger"
	case auditVerb(action) == "upload":
		return "info"
	}
	return "secondary"
}

func auditIcon(action string) string {
	switch {
	case action == "art.remove":
		return "x-square"
	case action == "trash.purge":
		return "fire"
	case auditVerb(action) == "modify":
		return "pencil-square"
	case auditVerb(action) == "import":
		return "box-arrow-in-down"
	case auditVerb(action) == "remove":
		return "trash"
	case auditVerb(action) == "upload":
		return "image"
	}
	return "journal-text"
}

func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return strings.ToUpper(string(r)) + s[n:]
}

// initial is the first letter of a name, for the round marker beside it.
func initial(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return ""
	}
	return strings.ToUpper(string(r))
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asList(v any) []any         { l, _ := v.([]any); return l }
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func kbText(v any) string {
	var n float64
	switch x := v.(type) {
	case json.Number:
		n, _ = x.Float64()
	case float64:
		n = x
	}
	return fmt.Sprintf("%.0f KB", n/1024)
}

// pairOf reads a change as {"old", "new"}, as Modifications and art paths are.
func pairOf(v any) (o, n any, ok bool) {
	m := asMap(v)
	if len(m) != 2 {
		return nil, nil, false
	}
	o, hasOld := m["old"]
	n, hasNew := m["new"]
	return o, n, hasOld && hasNew
}

// removedName names what a removal took out of the Library.
func removedName(removed map[string]any) string {
	if a := asMap(removed["album"]); a != nil { // undoing an Import keeps the Album and its Items
		removed = a
	}
	for _, k := range []string{"album", "title"} {
		if s := asString(removed[k]); s != "" {
			return s
		}
	}
	return ""
}

// auditSummary says in one line what an entry changed.
func auditSummary(e auditEntry) string {
	c := e.Changes
	switch {
	case strings.HasSuffix(e.Action, ".modify"):
		var bits []string
		moved := false
		for _, k := range sortedKeys(c) {
			o, n, ok := pairOf(c[k])
			switch {
			case !ok:
			case k == "path":
				moved = true
			default:
				bits = append(bits, k+" "+auditValue(o)+" → "+auditValue(n))
			}
		}
		switch {
		case len(bits) > 2:
			return strings.Join(bits[:2], "; ") + fmt.Sprintf(" (+%d more)", len(bits)-2)
		case len(bits) > 0:
			return strings.Join(bits, "; ")
		case moved:
			return "moved the file"
		}
		return "no field changes"
	case strings.HasSuffix(e.Action, ".import"):
		files := 1
		if items := asList(c["items"]); items != nil {
			files = len(items)
		}
		s := plural(files, "file", "files")
		if src := asList(c["source"]); len(src) > 0 {
			s += " from " + path.Base(asString(src[0])) + "/"
		}
		if m := asMap(c["match"]); m != nil {
			return s + ", matched on " + asString(m["data_source"])
		}
		return s + ", imported as-is"
	case strings.HasSuffix(e.Action, ".remove") && asMap(c["removed"]) != nil:
		name := removedName(asMap(c["removed"]))
		if n, ok := c["undo_of_import"]; ok {
			return fmt.Sprintf("undid Import %s: removed “%s”", valueString(n), name)
		}
		if by, ok := c["replaced_by"]; ok {
			return fmt.Sprintf("removed “%s” (replaced by %s)", name, asString(by))
		}
		s := fmt.Sprintf("removed “%s”, moved %s to the Trash", name, plural(len(asList(c["trashed_to"])), "file", "files"))
		if left := len(asList(c["left_in_place"])); left > 0 {
			s += fmt.Sprintf(", left %d outside the music directory", left)
		}
		return s
	case e.Action == "art.upload":
		_, n, _ := pairOf(c["artpath"])
		return fmt.Sprintf("set %s, %s %s", path.Base(asString(n)), kbText(c["bytes"]), asString(c["format"]))
	case e.Action == "trash.purge":
		files := 0
		if n, err := strconv.Atoi(valueString(c["files"])); err == nil {
			files = n
		}
		size, _ := strconv.ParseInt(valueString(c["bytes"]), 10, 64)
		return fmt.Sprintf("purged %s · %s", plural(files, "file", "files"), bytesText(size))
	case e.Action == "art.remove":
		o, _, _ := pairOf(c["artpath"])
		if len(asList(c["trashed_to"])) > 0 {
			return "removed " + path.Base(asString(o)) + ", moved to the Trash"
		}
		return "removed " + path.Base(asString(o))
	}
	return ""
}

// diffText is a value split into what it shares with the one beside it and
// what differs, so a long path shows only the part that changed.
type diffText struct{ Pre, Mid, Post string }

func textDiff(a, b string) (diffText, diffText) {
	ra, rb := []rune(a), []rune(b)
	i := 0
	for i < len(ra) && i < len(rb) && ra[i] == rb[i] {
		i++
	}
	j := 0
	for j < len(ra)-i && j < len(rb)-i && ra[len(ra)-1-j] == rb[len(rb)-1-j] {
		j++
	}
	cut := func(r []rune) diffText {
		return diffText{string(r[:i]), string(r[i : len(r)-j]), string(r[len(r)-j:])}
	}
	return cut(ra), cut(rb)
}

type fileRow struct {
	ID   string
	Path string
}

// changeView is one change of an entry, by the kind of value it holds.
type changeView struct {
	Key  string
	Kind string // pair, lines, files, rows, text
	// pair
	Old, New           diffText
	OldEmpty, NewEmpty bool
	// the rest
	Lines []string
	Files []fileRow
	Moves [][2]string // files moved to the Trash: from, to
	Rows  [][2]string
	Text  string
}

func compactJSON(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// changeViews lays out an entry's changes: old → new where a value changed,
// and lists, files and fields as a person would read them.
func changeViews(changes map[string]any) []changeView {
	var out []changeView
	for _, k := range sortedKeys(changes) {
		v := changes[k]
		c := changeView{Key: k}
		if o, n, ok := pairOf(v); ok {
			os, ns := auditValue(o), auditValue(n)
			c.Kind, c.OldEmpty, c.NewEmpty = "pair", os == "", ns == ""
			if c.OldEmpty || c.NewEmpty {
				c.Old, c.New = diffText{Mid: os}, diffText{Mid: ns}
			} else {
				c.Old, c.New = textDiff(os, ns)
			}
			out = append(out, c)
			continue
		}
		switch x := v.(type) {
		case []any:
			c = listChange(c, x)
		case map[string]any:
			c.Kind = "rows"
			for _, kk := range sortedKeys(x) {
				c.Rows = append(c.Rows, [2]string{kk, compactJSON(x[kk])})
			}
		default:
			c.Kind, c.Text = "text", auditValue(v)
			if s, ok := v.(string); ok && s == "asis" {
				c.Text = "as-is (no match)"
			}
		}
		out = append(out, c)
	}
	return out
}

func listChange(c changeView, l []any) changeView {
	if len(l) == 0 {
		c.Kind, c.Text = "text", "none"
		return c
	}
	var moves [][2]string
	for _, i := range l {
		m := asMap(i)
		if m == nil || asString(m["from"]) == "" || asString(m["to"]) == "" {
			moves = nil
			break
		}
		moves = append(moves, [2]string{asString(m["from"]), asString(m["to"])})
	}
	if moves != nil {
		c.Kind, c.Moves = "moves", moves
		return c
	}
	var files []fileRow
	for _, i := range l {
		m := asMap(i)
		if m == nil || asString(m["path"]) == "" {
			files = nil
			break
		}
		files = append(files, fileRow{valueString(m["id"]), asString(m["path"])})
	}
	if files != nil {
		c.Kind, c.Files = "files", files
		return c
	}
	c.Kind = "lines"
	for _, i := range l {
		c.Lines = append(c.Lines, compactJSON(i))
	}
	return c
}

// auditEntryPage is one entry and the other entries for what it changed.
type auditEntryPage struct {
	Entry        auditEntry
	History      []auditEntry // newest first, including this one
	HistoryTotal int
}

func (p auditEntryPage) MoreHistory() int { return p.HistoryTotal - len(p.History) }
