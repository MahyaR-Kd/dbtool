package dashboard

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"dbtool/internal/job"
	"dbtool/internal/types"
	"dbtool/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func testModel(t *testing.T) (*model, *[]string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	ui.SetNoColor(true)
	t.Cleanup(func() { ui.SetNoColor(false) })
	var ran []string
	m := newModel(Options{Version: "test", Command: func(args ...string) *exec.Cmd {
		ran = append(ran, strings.Join(args, " "))
		return exec.Command("true")
	}})
	m.configs = []types.Config{
		{Name: "prod", Host: "db.internal", Port: "3306", User: "backup", SSH: true, SSHUser: "ops", SSHHost: "bastion", SSHPort: "22", RetentionDays: 7, Password: "x"},
		{Name: "staging", Host: "10.0.0.5", Port: "3306", User: "root", IgnoredSchemas: []string{"tmp"}},
	}
	m.jobs = []job.Config{{Name: "nightly", Type: "sync", Schedule: "0 2 * * *", SrcConfigName: "prod", DstConfigName: "staging"}}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return m, &ran
}

func TestViewRendersEveryTab(t *testing.T) {
	m, _ := testModel(t)
	wants := map[tab][]string{
		tabConfigs:  {"dbtool", "prod", "staging", "SSH tunnel via ops@bastion:22", "7 days", "saved (encrypted)"},
		tabJobs:     {"nightly", "sync", "0 2 * * *", "Next run"},
		tabSettings: {"Work dir", "Storage", "local"},
		tabLogs:     {"No log file yet"},
	}
	for tb, want := range wants {
		m.tab = tb
		m.onTabChange()
		v := m.View()
		for _, w := range want {
			if !strings.Contains(v, w) {
				t.Errorf("tab %s: view missing %q\n%s", tabNames[tb], w, v)
			}
		}
		if n := strings.Count(v, "\n") + 1; n > 24 {
			t.Errorf("tab %s: view is %d lines, taller than the 24-line terminal", tabNames[tb], n)
		}
	}
	if os.Getenv("DASHBOARD_DUMP") != "" {
		m.tab = tabConfigs
		t.Log("\n" + m.View())
		m.tab = tabJobs
		t.Log("\n" + m.View())
	}
}

func press(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestActionsLaunchCommands(t *testing.T) {
	m, ran := testModel(t)
	m.Update(press("down")) // staging
	if _, cmd := m.Update(press("d")); cmd == nil {
		t.Fatal("dump should return an exec command")
	}
	if got := (*ran)[len(*ran)-1]; got != "dump --name staging" {
		t.Fatalf("ran %q", got)
	}

	// Delete asks first; "n" cancels, "y" runs.
	m.Update(press("x"))
	if m.confirm == nil {
		t.Fatal("delete should ask for confirmation")
	}
	m.Update(press("n"))
	if m.confirm != nil || len(*ran) != 1 {
		t.Fatal("n should cancel without running anything")
	}
	m.Update(press("x"))
	m.Update(press("y"))
	if got := (*ran)[len(*ran)-1]; got != "config delete --name staging" {
		t.Fatalf("ran %q", got)
	}

	m.Update(press("2"))
	if m.tab != tabJobs {
		t.Fatalf("tab = %v", m.tab)
	}
	m.Update(press("tab"))
	if m.tab != tabSettings {
		t.Fatalf("tab = %v", m.tab)
	}
}
