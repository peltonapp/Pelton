package desktop

import (
	"strconv"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scripting/spaces"
)

func taskTitles(t *testing.T, a *App) []string {
	t.Helper()
	list, err := a.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, task := range list {
		out = append(out, task.Title)
	}
	return out
}

func enabledScript(t *testing.T, a *App, name, trigger, src string, perms ...string) ScriptDTO {
	t.Helper()
	in := luaInput(src, perms...)
	in.Name, in.Trigger = name, ScriptTriggerDTO{Type: trigger}
	sc, err := a.CreateScript(in)
	if err != nil {
		t.Fatal(err)
	}
	if sc, err = a.SetScriptEnabled(sc.ID, true, sc.Permissions); err != nil {
		t.Fatal(err)
	}
	return sc
}

// settledRuns waits until the scripts' runs stop changing count.
func settledRuns(t *testing.T, a *App) []ScriptRunDTO {
	t.Helper()
	var runs []ScriptRunDTO
	for range 50 {
		time.Sleep(100 * time.Millisecond)
		next, err := a.ScriptRuns(0, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(next) == len(runs) && len(runs) > 0 {
			return runs
		}
		runs = next
	}
	return runs
}

func TestScriptOwnTaskDoesNotRetriggerIt(t *testing.T) {
	a, _ := newScriptTestApp(t)
	list, err := a.CreateTaskList("Inbox", "")
	if err != nil {
		t.Fatal(err)
	}
	enabledScript(t, a, "echo", spaces.TriggerTaskCreated,
		`tasks.create({title = "echo " .. event.task.title, list_id = event.task.list_id})`,
		spaces.PermTasksRead, spaces.PermTasksWrite)
	if _, err := a.CreateTask(TaskRequest{ListID: list.ID, Title: "x"}); err != nil {
		t.Fatal(err)
	}
	runs := settledRuns(t, a)
	titles := taskTitles(t, a)
	if len(runs) != 1 || runs[0].Status != "ok" || len(titles) != 2 {
		t.Fatalf("runs %+v titles %v", runs, titles)
	}
}

func TestScriptsAcrossSpacesStopAtMaxDepth(t *testing.T) {
	a, _ := newScriptTestApp(t)
	list, err := a.CreateTaskList("Inbox", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateLocalCalendar("Home", ""); err != nil {
		t.Fatal(err)
	}
	enabledScript(t, a, "task to event", spaces.TriggerTaskCreated,
		`calendar.create({title = event.task.title, start = "1d"})`,
		spaces.PermTasksRead, spaces.PermCalendarWrite)
	enabledScript(t, a, "event to task", spaces.TriggerEventCreated,
		`tasks.create({title = event.event.title, list_id = `+strconv.FormatInt(list.ID, 10)+`})`,
		spaces.PermCalendarRead, spaces.PermTasksWrite)
	if _, err := a.CreateTask(TaskRequest{ListID: list.ID, Title: "ping"}); err != nil {
		t.Fatal(err)
	}
	runs := settledRuns(t, a)
	skipped := 0
	for _, r := range runs {
		if r.Status == string(scripting.StatusSkipped) {
			skipped++
		}
	}
	if skipped == 0 || len(runs) > scripting.MaxDepth+3 {
		t.Fatalf("runs %+v", runs)
	}
}

func TestScriptFolderGrantStaysOnThisDevice(t *testing.T) {
	a, _ := newScriptTestApp(t)
	sc, err := a.CreateScript(luaInput(`files.read("a.txt")`, "files.read"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := a.setScriptFolder(sc.ID, dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.ScriptFolder(sc.ID); got != dir {
		t.Fatalf("folder %q", got)
	}
	settings, err := a.exportSettings()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := settings[settingScriptFolders]; ok {
		t.Fatal("a backup carries the folder grants")
	}
	if err := a.DeleteScript(sc.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.ScriptFolder(sc.ID); got != "" {
		t.Fatalf("grant outlived the script: %q", got)
	}
}

func TestFormatNoteDate(t *testing.T) {
	day := time.Date(2026, 11, 3, 9, 5, 0, 0, time.Local)
	for format, want := range map[string]string{
		"":                  "2026-11-03",
		"YYYY-MM-DD":        "2026-11-03",
		"DD.MM.YY":          "03.11.26",
		"dddd, MMMM Do":     "Tuesday, November 3rd",
		"[Week] ww, YYYY":   "Week 45, 2026",
		"YYYY/MMM/D h:mm a": "2026/Nov/3 9:05 am",
	} {
		if got := formatNoteDate(day, format); got != want {
			t.Errorf("%q: %q, want %q", format, got, want)
		}
	}
}
