package main

import (
	"net/http"
	"strings"
	"testing"
)

const root = "/in/Music"

func choiceOf(folder, action string, actor any) map[string]any {
	return map[string]any{"paths": []string{root + "/" + folder}, "kind": "album", "action": action, "actor": actor}
}

// progressImport is an Import waiting on its third Task, with two done.
func progressImport(extra map[string]any, choices ...any) map[string]any {
	prompt := albumPrompt()["result"].(map[string]any)["prompt"].(map[string]any)
	prompt["paths"] = []string{root + "/A/New"}
	result := map[string]any{"choices": choices, "prompt": prompt, "expected_tasks": 50, "tasks_done": 2, "actor": "alex"}
	for k, v := range extra {
		result[k] = v
	}
	return map[string]any{"id": 7, "status": "waiting_for_choice", "request": map[string]any{"paths": []string{root}}, "result": result}
}

func twoDone() []any {
	return []any{choiceOf("Afro/Distinto", "apply", "foobar"), choiceOf("Air/Moon", "apply", "alice")}
}

func pendingRoute(f *fakeAPI, total int, folders ...string) {
	f.routes["GET /imports/7/tasks"] = func(w http.ResponseWriter, r *http.Request) {
		list := []any{}
		for _, name := range folders {
			list = append(list, map[string]any{"paths": []string{root + "/" + name}, "state": "pending"})
		}
		writeJSON(w, 200, map[string]any{"total": total, "results": list})
	}
}

func serveImport(f *fakeAPI, op map[string]any) {
	f.routes["GET /imports/7"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, op) }
}

func TestNowShowsWhichTaskItIsOnOutOfHowManyAndWhatIsNext(t *testing.T) {
	f, h := newTest(t)
	serveImport(f, progressImport(nil, twoDone()...))
	pendingRoute(f, 47, "BTS/BE", "Coldplay/Parachutes")
	rec := get(t, h, "/imports/7")
	mustContain(t, rec, "Task 3 of 50", `aria-valuenow="6"`, "6%", "2 applied", "0 as-is", "0 skipped", "47 to go",
		"Up next:", "BTS/BE", "Match this Album.")
	r, _ := f.last(t, "GET", "/imports/7/tasks")
	if q := r.URL.Query(); q.Get("state") != "pending" || q.Get("limit") != "1" {
		t.Errorf("asked for %v", q)
	}
}

func TestNowWithoutATotalSaysTaskNumberAndAsksForNoList(t *testing.T) {
	f, h := newTest(t)
	serveImport(f, progressImport(map[string]any{"expected_tasks": nil}, twoDone()...))
	rec := get(t, h, "/imports/7")
	mustContain(t, rec, "Task 3", "progress-bar-striped", "2 applied")
	if strings.Contains(rec.Body.String(), "Task 3 of") || strings.Contains(rec.Body.String(), "to go") || strings.Contains(rec.Body.String(), "Up next") {
		t.Error("claims a total it doesn't have")
	}
	for _, r := range f.requests {
		if strings.HasSuffix(r.URL.Path, "/tasks") {
			t.Error("asked for a Task list for an Import with no plan")
		}
	}
}

func TestNowOverrunningTheEstimateSaysSo(t *testing.T) {
	f, h := newTest(t)
	serveImport(f, progressImport(map[string]any{"expected_tasks": 2, "tasks_done": 51}))
	mustContain(t, get(t, h, "/imports/7"), "Task 52 (2 were expected)", `aria-valuenow="100"`)
}

func TestNowWhileBeetsWorksSaysSoAndKeepsItsTabWhenRefreshing(t *testing.T) {
	f, h := newTest(t)
	op := progressImport(nil, twoDone()...)
	op["status"] = "running"
	op["result"].(map[string]any)["prompt"] = nil
	serveImport(f, op)
	pendingRoute(f, 48, "Air/Moon")
	mustContain(t, get(t, h, "/imports/7"), "beets is working", "Task 3 of 50", `hx-get="/imports/7"`, `hx-trigger="every 1s"`)
	mustContain(t, get(t, h, "/imports/7?tab=log&show=applied"), `hx-get="/imports/7?show=applied&amp;tab=log"`)
}

func TestLogListsTheCurrentTaskThenDoneNewestFirstThenWhatIsToCome(t *testing.T) {
	f, h := newTest(t)
	choices := append(twoDone(), choiceOf("Skipped/One", "skip", "foobar"), choiceOf("Plain/Two", "asis", nil))
	op := progressImport(map[string]any{"tasks_done": 4}, choices...)
	serveImport(f, op)
	pendingRoute(f, 45, "Q/1", "Q/2")
	rec := get(t, h, "/imports/7?tab=log")
	body := rec.Body.String()
	mustContain(t, rec, "Log <span class=\"badge text-bg-secondary\">4</span>", "Waiting for you", "A/New", "Afro/Distinto", "Air/Moon", "Skipped/One", "Plain/Two",
		"Applied", "Used as-is", "Skipped", "alice", "Waiting", "Q/1", "… 43 more", `href="/imports/7?show=remaining&amp;tab=log"`)
	order := []string{"A/New", "Plain/Two", "Skipped/One", "Air/Moon", "Afro/Distinto", "Q/1", "Q/2"}
	prev := -1
	for _, s := range order {
		i := strings.Index(body, s)
		if i < prev {
			t.Fatalf("%q is out of order", s)
		}
		prev = i
	}
	// Folders read under their Import Path, and the pending ones continue the numbering after the current Task (5).
	if strings.Contains(body, root+"/Afro") || !strings.Contains(body, `<td class="text-end">6</td>`) || !strings.Contains(body, `<td class="text-end">7</td>`) {
		t.Error("folders or numbers are wrong")
	}
	r, _ := f.last(t, "GET", "/imports/7/tasks")
	if r.URL.Query().Get("limit") != "100" {
		t.Errorf("asked for %v", r.URL.Query())
	}
}

func TestLogFiltersByHowTasksWent(t *testing.T) {
	f, h := newTest(t)
	choices := append(twoDone(), choiceOf("Skipped/One", "skip", "foobar"), choiceOf("Plain/Two", "asis", nil))
	serveImport(f, progressImport(map[string]any{"tasks_done": 4}, choices...))
	rec := get(t, h, "/imports/7?tab=log&show=skipped")
	mustContain(t, rec, "Skipped/One", `class="chip on" href="/imports/7?show=skipped&amp;tab=log"`)
	for _, gone := range []string{"Afro/Distinto", "Plain/Two", "Waiting for you"} {
		if strings.Contains(rec.Body.String(), gone) {
			t.Errorf("%s in the Skipped filter", gone)
		}
	}
	mustContain(t, get(t, h, "/imports/7?tab=log&show=asis"), "Plain/Two")
}

func TestRemainingFilterPagesThroughWhatIsToCome(t *testing.T) {
	f, h := newTest(t)
	serveImport(f, progressImport(nil, twoDone()...))
	pendingRoute(f, 250, "Q/1")
	rec := get(t, h, "/imports/7?tab=log&show=remaining&offset=100")
	mustContain(t, rec, "Q/1", "Earlier", "Later", `href="/imports/7?offset=200&amp;show=remaining&amp;tab=log"`, `href="/imports/7?show=remaining&amp;tab=log"`,
		"In the order beets will ask", `<td class="text-end">104</td>`)
	r, _ := f.last(t, "GET", "/imports/7/tasks")
	if q := r.URL.Query(); q.Get("offset") != "100" || q.Get("limit") != "100" || q.Get("state") != "pending" {
		t.Errorf("asked for %v", q)
	}
	if strings.Contains(rec.Body.String(), "Afro/Distinto") {
		t.Error("done Tasks in the Remaining filter")
	}
}

func TestPageWorksWithoutTheTaskList(t *testing.T) {
	f, h := newTest(t)
	serveImport(f, progressImport(nil, twoDone()...))
	// beets-api answers 404 for /tasks: the page still shows where it is.
	rec := get(t, h, "/imports/7")
	mustContain(t, rec, "Task 3 of 50", "47 to go", "Match this Album.")
	if strings.Contains(rec.Body.String(), "Up next") {
		t.Error("an Up next with no list")
	}
	rec = get(t, h, "/imports/7?tab=log")
	mustContain(t, rec, "Afro/Distinto", "Waiting for you")
}

func TestSearchingAndAnotherPromptForTheSameFolderIsOneTask(t *testing.T) {
	f, h := newTest(t)
	// The current Task has had a search; it is not a done Task yet.
	choices := append(twoDone(), choiceOf("A/New", "search", "foobar"))
	serveImport(f, progressImport(nil, choices...))
	rec := get(t, h, "/imports/7?tab=log")
	mustContain(t, rec, "Log <span class=\"badge text-bg-secondary\">2</span>", "Waiting for you")
	if strings.Count(rec.Body.String(), "A/New") != 1 {
		t.Error("the Task being asked about is listed as done too")
	}
}

func TestPathsTabSaysWhatWasAskedFor(t *testing.T) {
	f, h := newTest(t)
	serveImport(f, progressImport(nil))
	rec := get(t, h, "/imports/7?tab=paths")
	mustContain(t, rec, "Import Paths", root, "Albums", "Started by", "alex", "Tasks expected", "50", "an estimate")
	if strings.Contains(rec.Body.String(), "Match this Album.") {
		t.Error("the prompt is on the Paths tab")
	}
}

func TestFinishedImportSummarisesAndListsNothingToCome(t *testing.T) {
	f, h := newTest(t)
	op := progressImport(map[string]any{"tasks_done": 3, "outcomes": []any{
		map[string]any{"paths": []string{root + "/A"}, "skipped": false, "items": []any{map[string]any{"id": 11, "album_id": 3}}}}},
		twoDone()...)
	op["status"] = "completed"
	op["result"].(map[string]any)["prompt"] = nil
	serveImport(f, op)
	rec := get(t, h, "/imports/7")
	mustContain(t, rec, "Finished: 3 Tasks", `href="/albums/3"`, "2 applied")
	if strings.Contains(rec.Body.String(), "to go") || strings.Contains(rec.Body.String(), "Up next") || strings.Contains(get(t, h, "/imports/7?tab=log").Body.String(), "Remaining") {
		t.Error("a finished Import has things to come")
	}
	for _, r := range f.requests {
		if strings.HasSuffix(r.URL.Path, "/tasks") {
			t.Error("asked for the Tasks of a finished Import")
		}
	}
}

func TestRelativeFolders(t *testing.T) {
	for _, c := range []struct {
		roots      []string
		path, want string
	}{
		{[]string{"/in/Music"}, "/in/Music/Air/Moon", "Air/Moon"},
		{[]string{"/in/Music/"}, "/in/Music/Air", "Air"},
		{[]string{"/a", "/in/Music"}, "/in/Music/X", "X"},
		{[]string{"/in/Music"}, "/in/Music", "/in/Music"},       // the root itself
		{[]string{"/in/Mus"}, "/in/Music/Air", "/in/Music/Air"}, // only a prefix of the name
	} {
		if got := relativeTo(c.roots, c.path); got != c.want {
			t.Errorf("%v %s: %q, want %q", c.roots, c.path, got, c.want)
		}
	}
}

func TestLogShowsEveryTaskToComeUnlessThereAreHundreds(t *testing.T) {
	f, h := newTest(t)
	serveImport(f, progressImport(map[string]any{"expected_tasks": 12}, twoDone()...))
	var names []string
	for i := 1; i <= 9; i++ {
		names = append(names, "Q/"+string(rune('0'+i)))
	}
	pendingRoute(f, 9, names...)
	rec := get(t, h, "/imports/7?tab=log")
	for _, n := range names {
		mustContain(t, rec, n)
	}
	if strings.Contains(rec.Body.String(), "more</a>") {
		t.Error("a 'more' link when the whole list is shown")
	}
	// With hundreds, the first hundred show and the rest are one click away.
	pendingRoute(f, 250, names...)
	mustContain(t, get(t, h, "/imports/7?tab=log"), "… 241 more")
}
