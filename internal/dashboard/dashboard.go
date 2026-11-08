// Package dashboard is dbtool's optional full-screen terminal UI, opened
// with "dbtool ui". It shows configs, scheduled jobs, settings and the log
// in one place, and launches the regular commands (dump, restore, add,
// edit, delete) from there.
//
// Actions are not reimplemented here: each one suspends the dashboard and
// runs the normal dbtool command in the same terminal (tea.ExecProcess),
// so forms, password prompts, progress bars and confirmations behave
// exactly as they do on the command line. When the command finishes the
// dashboard reloads its data and comes back.
package dashboard

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"dbtool/internal/config"
	"dbtool/internal/job"
	"dbtool/internal/logger"
	"dbtool/internal/settings"
	"dbtool/internal/types"
	"dbtool/internal/ui"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Options configures the dashboard.
type Options struct {
	// Version is shown in the header.
	Version string
	// Command builds the process for a dbtool subcommand, e.g.
	// Command("dump", "--name", "prod"). It should pause for Enter when the
	// command ends so its output can be read before the dashboard returns.
	Command func(args ...string) *exec.Cmd
}

// Run opens the dashboard and blocks until the user quits.
func Run(opts Options) error {
	m := newModel(opts)
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

type tab int

const (
	tabConfigs tab = iota
	tabJobs
	tabSettings
	tabLogs
)

var tabNames = []string{"Configs", "Jobs", "Settings", "Logs"}

type model struct {
	opts          Options
	tab           tab
	width, height int

	configs  []types.Config
	jobs     []job.Config
	settings settings.Settings

	cfgCursor, jobCursor int

	logView   viewport.Model
	logFollow bool
	logReady  bool

	status     string
	statusKind int
	statusAt   time.Time

	confirm *confirmation

	keys keyMap
	help help.Model
}

type confirmation struct {
	prompt string
	args   []string
	done   string
}

type (
	tickMsg     time.Time
	execDoneMsg struct {
		label string
		err   error
	}
)

func newModel(opts Options) *model {
	m := &model{opts: opts, keys: newKeyMap(), help: help.New(), logFollow: true}
	m.help.Styles.ShortKey = lipgloss.NewStyle().Foreground(ui.ColorAccent)
	m.help.Styles.ShortDesc = lipgloss.NewStyle().Foreground(ui.ColorMuted)
	m.help.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(ui.ColorMuted)
	m.reload()
	return m
}

func (m *model) reload() {
	m.configs = config.Load()
	sort.SliceStable(m.configs, func(i, j int) bool { return m.configs[i].Name < m.configs[j].Name })
	m.jobs = job.Load()
	sort.SliceStable(m.jobs, func(i, j int) bool { return m.jobs[i].Name < m.jobs[j].Name })
	m.settings = settings.Load()
	m.cfgCursor = clamp(m.cfgCursor, 0, len(m.configs)-1)
	m.jobCursor = clamp(m.jobCursor, 0, len(m.jobs)-1)
	m.loadLog()
}

func tick() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) Init() tea.Cmd { return tick() }

// ---------------------------------------------------------------- keys

type keyMap struct {
	Up, Down, Tab, ShiftTab, Quit, Help    key.Binding
	Dump, Restore, Add, Edit, Delete, Copy key.Binding
	Follow, Top, Bottom, Reload            key.Binding
	Yes, No                                key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Tab:      key.NewBinding(key.WithKeys("tab", "right", "l"), key.WithHelp("tab", "next tab")),
		ShiftTab: key.NewBinding(key.WithKeys("shift+tab", "left", "h"), key.WithHelp("shift+tab", "prev tab")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Dump:     key.NewBinding(key.WithKeys("d", "enter"), key.WithHelp("d", "dump")),
		Restore:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "restore")),
		Add:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Delete:   key.NewBinding(key.WithKeys("x", "delete"), key.WithHelp("x", "delete")),
		Copy:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "duplicate")),
		Follow:   key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "follow")),
		Top:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom:   key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		Reload:   key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "reload")),
		Yes:      key.NewBinding(key.WithKeys("y", "Y"), key.WithHelp("y", "yes")),
		No:       key.NewBinding(key.WithKeys("n", "N", "esc", "q"), key.WithHelp("n", "no")),
	}
}

func (m *model) shortHelp() []key.Binding {
	k := m.keys
	if m.confirm != nil {
		return []key.Binding{k.Yes, k.No}
	}
	switch m.tab {
	case tabConfigs:
		return []key.Binding{k.Up, k.Down, k.Dump, k.Restore, k.Add, k.Edit, k.Copy, k.Delete, k.Tab, k.Quit}
	case tabJobs:
		return []key.Binding{k.Up, k.Down, k.Add, k.Edit, k.Delete, k.Tab, k.Quit}
	case tabLogs:
		return []key.Binding{k.Up, k.Down, k.Top, k.Bottom, k.Follow, k.Tab, k.Quit}
	}
	return []key.Binding{k.Tab, k.Reload, k.Quit}
}

// ---------------------------------------------------------------- update

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeLog()
		return m, nil

	case tickMsg:
		if m.tab == tabLogs {
			m.loadLog()
		}
		if !m.statusAt.IsZero() && time.Since(m.statusAt) > 8*time.Second {
			m.status = ""
		}
		return m, tick()

	case execDoneMsg:
		m.reload()
		if msg.err != nil {
			m.setStatus(fmt.Sprintf("%s exited with an error: %v", msg.label, msg.err), ui.BadgeError)
		} else {
			m.setStatus(msg.label+" finished", ui.BadgeSuccess)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	if m.tab == tabLogs {
		var cmd tea.Cmd
		m.logView, cmd = m.logView.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys

	if m.confirm != nil {
		c := m.confirm
		switch {
		case key.Matches(msg, k.Yes):
			m.confirm = nil
			return m, m.run(c.done, c.args...)
		case key.Matches(msg, k.No), msg.String() == "ctrl+c":
			m.confirm = nil
			m.setStatus("Canceled", ui.BadgeNeutral)
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, k.Quit):
		return m, tea.Quit
	case key.Matches(msg, k.Tab):
		m.tab = (m.tab + 1) % tab(len(tabNames))
		m.onTabChange()
		return m, nil
	case key.Matches(msg, k.ShiftTab):
		m.tab = (m.tab + tab(len(tabNames)) - 1) % tab(len(tabNames))
		m.onTabChange()
		return m, nil
	case key.Matches(msg, k.Reload):
		m.reload()
		m.setStatus("Reloaded", ui.BadgeNeutral)
		return m, nil
	}
	if s := msg.String(); len(s) == 1 && s[0] >= '1' && s[0] <= '4' {
		m.tab = tab(s[0] - '1')
		m.onTabChange()
		return m, nil
	}

	switch m.tab {
	case tabConfigs:
		return m.configKeys(msg)
	case tabJobs:
		return m.jobKeys(msg)
	case tabLogs:
		switch {
		case key.Matches(msg, k.Follow):
			m.logFollow = !m.logFollow
			if m.logFollow {
				m.logView.GotoBottom()
			}
			return m, nil
		case key.Matches(msg, k.Top):
			m.logFollow = false
			m.logView.GotoTop()
			return m, nil
		case key.Matches(msg, k.Bottom):
			m.logView.GotoBottom()
			return m, nil
		}
		var cmd tea.Cmd
		m.logView, cmd = m.logView.Update(msg)
		if key.Matches(msg, k.Up) {
			m.logFollow = false
		}
		return m, cmd
	}
	return m, nil
}

func (m *model) configKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	switch {
	case key.Matches(msg, k.Up):
		m.cfgCursor = clamp(m.cfgCursor-1, 0, len(m.configs)-1)
	case key.Matches(msg, k.Down):
		m.cfgCursor = clamp(m.cfgCursor+1, 0, len(m.configs)-1)
	case key.Matches(msg, k.Add):
		return m, m.run("config add", "config", "add")
	}
	if len(m.configs) == 0 {
		return m, nil
	}
	name := m.configs[m.cfgCursor].Name
	switch {
	case key.Matches(msg, k.Dump):
		return m, m.run("Dump of "+name, "dump", "--name", name)
	case key.Matches(msg, k.Restore):
		return m, m.run("Restore into "+name, "restore", "--name", name)
	case key.Matches(msg, k.Edit):
		return m, m.run("config edit", "config", "edit")
	case key.Matches(msg, k.Copy):
		return m, m.run("config duplicate", "config", "duplicate")
	case key.Matches(msg, k.Delete):
		m.confirm = &confirmation{
			prompt: fmt.Sprintf("Delete config %s?", ui.Bold(name)),
			args:   []string{"config", "delete", "--name", name},
			done:   "Delete of config " + name,
		}
	}
	return m, nil
}

func (m *model) jobKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	switch {
	case key.Matches(msg, k.Up):
		m.jobCursor = clamp(m.jobCursor-1, 0, len(m.jobs)-1)
	case key.Matches(msg, k.Down):
		m.jobCursor = clamp(m.jobCursor+1, 0, len(m.jobs)-1)
	case key.Matches(msg, k.Add):
		return m, m.run("job add", "job", "add")
	}
	if len(m.jobs) == 0 {
		return m, nil
	}
	name := m.jobs[m.jobCursor].Name
	switch {
	case key.Matches(msg, k.Edit):
		return m, m.run("job edit", "job", "edit")
	case key.Matches(msg, k.Delete):
		m.confirm = &confirmation{
			prompt: fmt.Sprintf("Delete job %s?", ui.Bold(name)),
			args:   []string{"job", "delete", "--name", name},
			done:   "Delete of job " + name,
		}
	}
	return m, nil
}

// run suspends the dashboard and runs a dbtool subcommand in the terminal.
func (m *model) run(label string, args ...string) tea.Cmd {
	if m.opts.Command == nil {
		m.setStatus("Running commands is not available here", ui.BadgeError)
		return nil
	}
	c := m.opts.Command(args...)
	return tea.ExecProcess(c, func(err error) tea.Msg { return execDoneMsg{label: label, err: err} })
}

func (m *model) setStatus(s string, kind int) {
	m.status, m.statusKind, m.statusAt = s, kind, time.Now()
}

func (m *model) onTabChange() {
	if m.tab == tabLogs {
		m.loadLog()
	}
}

// ---------------------------------------------------------------- log tab

const maxLogLines = 1000

func (m *model) resizeLog() {
	w, h := m.bodySize()
	if !m.logReady {
		m.logView = viewport.New(w, h)
		m.logReady = true
	} else {
		m.logView.Width, m.logView.Height = w, h
	}
	m.loadLog()
}

func (m *model) loadLog() {
	if !m.logReady {
		return
	}
	f, err := os.Open(logger.LogFile())
	if err != nil {
		m.logView.SetContent(ui.Muted("No log file yet — run some dbtool commands first."))
		return
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > maxLogLines*2 {
			lines = lines[len(lines)-maxLogLines:]
		}
	}
	if len(lines) > maxLogLines {
		lines = lines[len(lines)-maxLogLines:]
	}
	for i, l := range lines {
		lines[i] = colorLogLine(l)
	}
	atBottom := m.logView.AtBottom()
	m.logView.SetContent(strings.Join(lines, "\n"))
	if m.logFollow || atBottom {
		m.logView.GotoBottom()
	}
}

func colorLogLine(l string) string {
	// "2006-01-02 15:04:05 [LEVEL] message"
	if len(l) < 20 {
		return l
	}
	ts, rest := l[:19], l[19:]
	switch {
	case strings.HasPrefix(rest, " [ERROR]"):
		return ui.Muted(ts) + ui.Red(" [ERROR]") + rest[8:]
	case strings.HasPrefix(rest, " [WARN]"):
		return ui.Muted(ts) + ui.Yellow(" [WARN]") + rest[7:]
	case strings.HasPrefix(rest, " [INFO]"):
		return ui.Muted(ts) + ui.Blue(" [INFO]") + rest[7:]
	case strings.HasPrefix(rest, " [DEBUG]"):
		return ui.Muted(ts + " [DEBUG]" + rest[8:])
	}
	return l
}

// ---------------------------------------------------------------- view

func (m *model) bodySize() (int, int) {
	w := m.width - 4
	h := m.height - 7 // header, rules, status, help
	if w < 20 {
		w = 20
	}
	if h < 3 {
		h = 3
	}
	return w, h
}

func (m *model) View() string {
	if m.width == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n")
	b.WriteString(m.rule())
	b.WriteString("\n")

	_, h := m.bodySize()
	body := ""
	switch m.tab {
	case tabConfigs:
		body = m.configsView(h)
	case tabJobs:
		body = m.jobsView(h)
	case tabSettings:
		body = m.settingsView()
	case tabLogs:
		body = m.logView.View()
	}
	body = lipgloss.NewStyle().Padding(0, 2).Height(h).MaxHeight(h).Render(body)
	b.WriteString(body)
	b.WriteString("\n")
	b.WriteString(m.rule())
	b.WriteString("\n")
	b.WriteString(m.statusLine())
	b.WriteString("\n")
	b.WriteString(" " + m.help.ShortHelpView(m.shortHelp()))
	return b.String()
}

func (m *model) header() string {
	brand := lipgloss.NewStyle().Foreground(ui.ColorAccent).Bold(true).Render(" " + ui.Icon(ui.IconBrand) + " dbtool ")
	var tabs []string
	for i, name := range tabNames {
		label := fmt.Sprintf(" %d %s ", i+1, name)
		if tab(i) == m.tab {
			tabs = append(tabs, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(ui.ColorAccent).Render(label))
		} else {
			tabs = append(tabs, lipgloss.NewStyle().Foreground(ui.ColorMuted).Render(label))
		}
	}
	left := brand + " " + strings.Join(tabs, " ")
	right := ui.Muted(m.opts.Version + " ")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *model) rule() string {
	ch := "─"
	if ui.ASCII() {
		ch = "-"
	}
	return ui.Muted(strings.Repeat(ch, max(m.width, 1)))
}

func (m *model) statusLine() string {
	if m.confirm != nil {
		return " " + ui.Yellow(ui.Icon(ui.IconWarn)) + " " + m.confirm.prompt + "  " + ui.Muted("y / n")
	}
	if m.status == "" {
		return ""
	}
	icon := ui.Muted(ui.Icon(ui.IconInfo))
	switch m.statusKind {
	case ui.BadgeSuccess:
		icon = ui.Green(ui.Icon(ui.IconOK))
	case ui.BadgeError:
		icon = ui.Red(ui.Icon(ui.IconFail))
	}
	return " " + icon + " " + m.status
}

// listAndDetail renders a selectable list on the left and a detail pane on
// the right (stacked when the terminal is narrow).
func (m *model) listAndDetail(rows []string, cursor int, detail string, h int) string {
	w, _ := m.bodySize()
	listW := w * 2 / 5
	if listW < 24 {
		listW = 24
	}
	// Keep the cursor visible.
	start := 0
	if cursor >= h {
		start = cursor - h + 1
	}
	var lines []string
	for i := start; i < len(rows) && i < start+h; i++ {
		ptr := "  "
		row := rows[i]
		if i == cursor {
			ptr = ui.Accent("❯ ")
			if ui.ASCII() {
				ptr = ui.Accent("> ")
			}
			row = ui.Bold(row)
		}
		lines = append(lines, ptr+row)
	}
	list := lipgloss.NewStyle().Width(listW).MaxWidth(listW).Render(strings.Join(lines, "\n"))
	if w < 70 {
		return list
	}
	sep := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(ui.ColorMuted).
		PaddingLeft(2).
		Width(w - listW - 2).
		Height(h).
		Render(detail)
	return lipgloss.JoinHorizontal(lipgloss.Top, list, sep)
}

func (m *model) configsView(h int) string {
	if len(m.configs) == 0 {
		return emptyState("No configs yet", "Press a to add your first database connection.")
	}
	rows := make([]string, len(m.configs))
	for i, c := range m.configs {
		route := ""
		if c.SSH {
			route = ui.Muted(" ssh")
		}
		rows[i] = c.Name + route
	}
	return m.listAndDetail(rows, m.cfgCursor, configDetail(m.configs[m.cfgCursor]), h)
}

func configDetail(c types.Config) string {
	route := "direct"
	if c.SSH {
		route = fmt.Sprintf("SSH tunnel via %s@%s:%s", c.SSHUser, c.SSHHost, c.SSHPort)
	}
	retention := "keep forever"
	if c.RetentionDays > 0 {
		retention = ui.Plural(c.RetentionDays, "day", "days")
	}
	password := ui.Muted("not saved")
	if c.Password != "" {
		password = ui.Green("saved (encrypted)")
	}
	ignored := ""
	if len(c.IgnoredSchemas) > 0 {
		ignored = strings.Join(c.IgnoredSchemas, ", ")
	}
	var tables []string
	schemas := make([]string, 0, len(c.IgnoredTables))
	for s := range c.IgnoredTables {
		schemas = append(schemas, s)
	}
	sort.Strings(schemas)
	for _, s := range schemas {
		for _, t := range c.IgnoredTables[s] {
			tables = append(tables, s+"."+t)
		}
	}
	telegram := ui.OnOff(!c.TelegramDisabled)
	locks := ""
	if c.NoLocks {
		locks = "--no-locks"
	}
	return ui.Accent(c.Name) + "\n\n" + ui.KeyValues(
		ui.KV{Key: "Host", Value: c.Host + ":" + c.Port},
		ui.KV{Key: "User", Value: c.User},
		ui.KV{Key: "Route", Value: route},
		ui.KV{Key: "Retention", Value: retention},
		ui.KV{Key: "Password", Value: password},
		ui.KV{Key: "Telegram", Value: telegram},
		ui.KV{Key: "Locks", Value: locks},
		ui.KV{Key: "Ignore schemas", Value: ignored},
		ui.KV{Key: "Ignore tables", Value: strings.Join(tables, ", ")},
	)
}

func (m *model) jobsView(h int) string {
	if len(m.jobs) == 0 {
		return emptyState("No scheduled jobs yet", "Press a to schedule a recurring dump, restore or sync.")
	}
	rows := make([]string, len(m.jobs))
	for i, j := range m.jobs {
		rows[i] = j.Name + " " + jobBadge(j.Type)
	}
	return m.listAndDetail(rows, m.jobCursor, jobDetail(m.jobs[m.jobCursor]), h)
}

func jobBadge(t string) string {
	switch t {
	case "dump":
		return ui.Badge(t, ui.BadgeInfo)
	case "restore":
		return ui.Badge(t, ui.BadgeWarn)
	case "sync":
		return ui.Badge(t, ui.BadgeSync)
	}
	return ui.Badge(t, ui.BadgeNeutral)
}

func jobDetail(j job.Config) string {
	next := job.DescribeNext(j.Schedule, time.Now())
	if next == "" {
		next = ui.Muted("—")
	}
	src, dst := j.SrcConfigName, j.DstConfigName
	if j.Type == "dump" {
		dst = ""
	}
	overwrite := ""
	if j.Type != "dump" {
		overwrite = ui.OnOff(j.OverwriteTables)
	}
	return ui.Accent(j.Name) + "  " + jobBadge(j.Type) + "\n\n" + ui.KeyValues(
		ui.KV{Key: "Schedule", Value: j.Schedule},
		ui.KV{Key: "Next run", Value: next},
		ui.KV{Key: "Source", Value: src},
		ui.KV{Key: "Destination", Value: dst},
		ui.KV{Key: "Overwrite tables", Value: overwrite},
	)
}

func (m *model) settingsView() string {
	s := m.settings
	storage := "local"
	if s.S3Enabled() {
		storage = "s3://" + s.S3.Bucket + "/" + strings.TrimSuffix(s.S3.Prefix, "/")
	}
	proxy := ui.OnOff(false)
	if s.Proxy.Enabled() {
		proxy = ui.Green(ui.Icon(ui.IconOn)) + " " + s.Proxy.Host + ":" + s.Proxy.Port
	}
	tg := ui.OnOff(false)
	if s.Telegram.Enabled() {
		tg = ui.Green(ui.Icon(ui.IconOn)) + " chat " + s.Telegram.ChatID
	}
	return ui.Accent("Settings") + "\n\n" + ui.KeyValues(
		ui.KV{Key: "Work dir", Value: s.WorkDir},
		ui.KV{Key: "Storage", Value: storage},
		ui.KV{Key: "S3 encryption", Value: onOffIf(s.S3Enabled(), s.S3.EncryptionEnabled())},
		ui.KV{Key: "SOCKS5 proxy", Value: proxy},
		ui.KV{Key: "Telegram", Value: tg},
		ui.KV{Key: "Telegram encryption", Value: onOffIf(s.Telegram.Enabled(), s.Telegram.EncryptionEnabled())},
		ui.KV{Key: "Log file", Value: logger.LogFile()},
	) + "\n" + ui.Muted("Change settings with: dbtool setting <s3|proxy|telegram|workdir|master-password> …")
}

func onOffIf(applies, on bool) string {
	if !applies {
		return ""
	}
	return ui.OnOff(on)
}

func emptyState(title, hint string) string {
	return "\n" + ui.Muted(ui.Icon(ui.IconPending)) + " " + ui.Bold(title) + "\n  " + ui.Muted(hint)
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
