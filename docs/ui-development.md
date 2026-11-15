# Writing UI code in dbtool

How to print, prompt and show progress when you add or change a command.
The user-facing description of the interface is in [TUI.md](TUI.md).

## The rule

**Don't call `fmt.Print*` for user-facing output.** Use `github.com/MahyaR-Kd/dbtool/internal/ui`
for output and `github.com/MahyaR-Kd/dbtool/internal/interactivelist` for prompts. This keeps the
look consistent, routes errors to stderr, honors `--no-color` / `NO_COLOR`,
and keeps live progress bars from being garbled by other prints.

Exceptions: output meant to be parsed, such as a bare path from
`setting workdir get` or raw log lines, and the plain line-based prompt
fallbacks inside `interactivelist` itself.

## Package map

| Package | What it's for |
|---|---|
| `internal/ui` | Palette, icons, status messages, layout (title, section, key/values, table, box, badges), live elements (`Task`, `Bar`), formatting (`Duration`, `Bytes`, `Plural`) |
| `internal/interactivelist` | Prompts: `SelectOne`, `SelectMulti`, `Confirm`, `Text`, `TextValidated` + validators, `PromptLabel`; the huh theme (`Theme()`) |
| `internal/progress` | Parses mydumper/myloader output; `New`/`SpinnerBar` return a `*ui.Bar` |
| `internal/dashboard` | The `dbtool ui` full-screen Bubble Tea app |
| `cmd/help.go` | Help/usage templates, command groups, the welcome screen |

## Messages

```go
ui.Success("Config %s saved", ui.Bold(name))           // ✔  stdout
ui.Info("Using saved password for %s", name)           // ℹ  stdout
ui.Step("Installing mydumper…")                        // ➜  stdout
ui.Warn("Telegram delivery failed: %v", err)           // ⚠  stderr
ui.Error("Config not found: %s", name)                 // ✖  stderr
ui.Fatal("Cannot open config storage: %v", err)        // ✖  stderr + os.Exit(1)
ui.Hint("Try %s", ui.Code("dbtool setting s3 test"))   // dim "  › …"
ui.Detail("sent part %d/%d", i, n)                     // dim "  • …"
ui.Empty("No jobs yet", "Create one with "+ui.Code("dbtool job add"))
```

Guidelines:

- Don't put `Error:` or `Warning:` in the text. The icon says it.
- Follow an error with a `Hint` when there's a clear next step.
- Use `Fatal` only where the old code did `Println` + `os.Exit(1)`. It also
  clears any running spinner first.
- Inline styling: `ui.Bold`, `ui.Muted`, `ui.Accent`, `ui.Green`,
  `ui.Yellow`, `ui.Red`, `ui.Blue`, `ui.Code`. Each returns the plain string
  when colors are off.

## Layout

```go
ui.Title("Dump · "+cfg.Name, "")          // "◆ Dump · prod" + blank line
ui.Section("SSH tunnel")                  // "── SSH tunnel ─────────"

ui.PrintKeyValues(                        // aligned, keys dimmed,
    ui.KV{Key: "Host", Value: host},      // empty values skipped
    ui.KV{Key: "Telegram", Value: ui.OnOff(enabled)},
)

ui.PrintTable([]string{"NAME", "HOST"}, rows)   // header + rule, no grid

ui.PrintBox("Dump complete", ui.BadgeSuccess,   // rounded summary box
    ui.KeyValues(ui.KV{Key: "Took", Value: ui.Duration(d)}))

ui.Badge("sync", ui.BadgeSync)            // colored bold label
ui.Status(on, "background scheduling")    // "● label" / dim "○ label"
```

Badge kinds are `BadgeNeutral`, `BadgeAccent`, `BadgeSuccess`, `BadgeWarn`,
`BadgeError`, `BadgeInfo` and `BadgeSync`. `Box` uses the kind for its
border color and title icon.

## Long-running work

A **Task** is one stage with a spinner. It ends with a result line:

```go
t := ui.StartTask("Uploading to S3")
if err := upload(); err != nil {
    t.Fail(err)            // ✖ Uploading to S3  <err>  1.2s   (stderr)
    os.Exit(1)
}
t.Done(location)           // ✔ Uploading to S3  s3://…  4.1s
// also: t.Warn(msg), t.Skip(reason), t.Update("new label")
```

A **Bar** counts work:

```go
bar := ui.NewBar(total, "Dumping "+name, "tables") // total <= 0 → spinner
bar.Set(n)           // never goes backwards; safe from any goroutine
bar.SetTotal(tot)    // spinner → real bar, in place
bar.Finish()         // erase, print nothing; then report yourself, or:
bar.Done("Dumped "+name, ui.Plural(n, "table", "tables"))
```

Only one live element is shown at a time. Any `ui` print while it runs
erases the live line, prints, and redraws it below, so you can print from
other goroutines while a bar is running. Raw `fmt.Print` bypasses this and
breaks the display.

When stdout is not a terminal, live elements never animate: `Task` prints
only its result line and `Bar` prints nothing. This keeps cron output clean.

If you need to read from the terminal while a spinner might be running,
wrap the read with `resume := ui.Pause(); …; resume()`.

## Prompts

```go
r := bufio.NewReader(os.Stdin) // share one reader across a whole form

ui.Section("Connection")
name := interactivelist.TextValidated(r, "Name", cur.Name, interactivelist.Required)
port := interactivelist.TextValidated(r, "Port", cur.Port, interactivelist.Port)
useSSH := interactivelist.Confirm("Use SSH?", cur.SSH)

i, err := interactivelist.SelectOne("Choose a config", names)
if errors.Is(err, interactivelist.ErrCanceled) { … }

idxs, err := interactivelist.SelectMulti("Ignore schemas", schemas,
    func(i int) bool { return already[schemas[i]] })

pass, err := secureinput.ReadPassword(interactivelist.PromptLabel("DB password", ""))
```

- A validator is `func(string) error`. It gets the trimmed value, with an
  empty value already replaced by `current`. Built-in validators are
  `Required`, `Port`, `OptionalPort` and `NonNegativeInt`.
- In a terminal the prompts are huh fields. Elsewhere they fall back to the
  old line-based prompts reading from `r` / stdin, and those fallbacks are
  unit-tested (`interactivelist_test.go`). Keep both paths working.
- `Confirm` and `Text` exit with status 130 on `ctrl+c`, because they have no
  error return. `SelectOne`/`SelectMulti` return `ErrCanceled`.
- Interactive add/edit flows end with a `ui.PrintBox("Review", …)` and
  `Confirm("Save …?", true)`. Flag-driven paths must not prompt.

## Colors and icons

- `ui.ColorEnabled()` is false when stdout isn't a TTY, when `NO_COLOR` is
  set, when `--no-color` is passed (`ui.SetNoColor`), or when
  `DBTOOL_COLOR=never` is set. `DBTOOL_COLOR=always` forces colors on.
- Icons come from `ui.Icon(ui.IconOK)` and similar. `DBTOOL_ASCII=1` /
  `TERM=dumb` switch to ASCII (`ui.SetASCII` in tests). Avoid emoji in
  tables, because their width varies between terminals.
- Palette colors are `ui.ColorAccent`, `ColorSuccess`, `ColorWarn`,
  `ColorError`, `ColorInfo`, `ColorMuted`, `ColorText` and `ColorSync`. They
  are adaptive light/dark pairs. Build new lipgloss styles from these rather
  than hard-coding hex values.

## Help text

Commands get grouped help automatically through `commandGroupOf` in
[cmd/help.go](../cmd/help.go). Add a new top-level command there to place
it under *Backup & restore*, *Configs & jobs* or *Settings & maintenance*.
Write `Short` as a sentence fragment without a trailing period.

## The dashboard

`internal/dashboard` is a single Bubble Tea model. Actions don't call
internal packages directly. They run `dbtool _exec-wait <subcommand…>` via
`tea.ExecProcess`. That hidden command runs the subcommand and waits for
Enter. A new action is usually a single `m.run(label, args...)` line plus a
key binding. Keep it that way, so the dashboard never drifts from what the
CLI does.

## Testing UI code

- `ui` tests redirect `ui.Stdout`/`ui.Stderr` to buffers and call
  `ui.SetNoColor(true)`. Assert on plain text, and include a "no `\x1b[`"
  check for anything new.
- Prompt logic is tested through the fallback functions with fake readers.
- The dashboard is tested by sending `tea.KeyMsg`/`tea.WindowSizeMsg` to the
  model and asserting on `View()` and on the commands it would launch. Run
  `DASHBOARD_DUMP=1 go test ./internal/dashboard -run TestView -v` to print
  the rendered screens.
- For a manual check, run commands in a real terminal and again with
  `| cat`, with `NO_COLOR=1`, and with `DBTOOL_ASCII=1`.
