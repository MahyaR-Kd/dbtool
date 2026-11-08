# dbtool's terminal interface

This guide covers how dbtool looks and behaves in a terminal: the welcome
screen, prompts and pickers, progress output, list views, the full-screen
dashboard, and how to turn colors and icons off.

For contributors there is a separate guide on writing UI code:
[ui-development.md](ui-development.md).

- [Design principles](#design-principles)
- [Welcome screen and help](#welcome-screen-and-help)
- [Status messages and icons](#status-messages-and-icons)
- [Prompts, pickers and forms](#prompts-pickers-and-forms)
- [Dump and restore progress](#dump-and-restore-progress)
- [Lists and overviews](#lists-and-overviews)
- [The dashboard (`dbtool ui`)](#the-dashboard-dbtool-ui)
- [Colors, icons and scripting](#colors-icons-and-scripting)
- [Troubleshooting](#troubleshooting)

---

## Design principles

1. **Inline first.** Everything except the optional dashboard prints inline
   and stays in your scrollback. Prompts and pickers do not switch the
   terminal to a full-screen view, so you can scroll back later to see what
   was asked and answered.
2. **One visual vocabulary.** Every command uses the same icons, colors and
   layout from one package (`internal/ui`). A ✔ means the same thing in
   `config add` as it does in `dump`.
3. **Scripts are not affected.** When stdin/stdout is not a terminal (pipes,
   cron, CI), colors and animation turn off and prompts fall back to plain
   numbered and line-based input. Flag-driven, non-interactive commands
   behave exactly as before.
4. **Errors go to stderr.** Warnings and errors go to stderr; normal output
   goes to stdout. `dbtool config list > configs.txt` captures the list and
   still shows problems on screen.
5. **No extra binaries.** The picker is built in. `fzf` is no longer used or
   needed.

---

## Welcome screen and help

Running `dbtool` with no arguments shows a short status and the commands
people use most:

```
◆ dbtool  v1.4.0
  MySQL dump & restore · SSH tunnels · scheduled jobs · S3 & Telegram

  Configs    3
  Jobs       2
  Storage    s3://backups/mysql
  Telegram   ● on
  Scheduler  ● background scheduling

Get started
  dbtool dump         Dump a database
  dbtool restore      Restore a dump into a database
  dbtool job add      Schedule a recurring dump / restore / sync
  dbtool ui           Open the interactive dashboard

  › Run dbtool --help for every command
```

With no configs yet, the first suggestion is `dbtool config add`. If the
binary is not installed on your `PATH`, the screen tells you to run
`./dbtool install`.

`dbtool --help` groups commands by purpose, with colored headings and flag
names:

```
Usage
  dbtool <command> [flags]

Backup & restore
  decrypt    Decrypt an archive encrypted by dbtool's S3/Telegram encryption password
  dump       Dump database
  restore    Restore database
  validate   Check a dump directory for columns silently dropped from INSERT statements

Configs & jobs
  config     Manage database connection configs
  job        Manage scheduled dump/restore jobs
  schedule   Enable or disable background scheduling via crontab
  ui         Open the interactive dashboard (configs, jobs, settings, logs)

Settings & maintenance
  log        Show dbtool log entries
  setting    Manage dbtool settings (storage, working directory, proxy)
```

A mistyped command prints one error line and a hint, not the full usage
text:

```
✖ unknown command "dumpp" for "dbtool"
  › Run dbtool --help to see all commands
```

---

## Status messages and icons

| Icon | Meaning | Stream |
|:---:|---|---|
| `✔` (green) | Something finished successfully | stdout |
| `ℹ` (blue) | Neutral information | stdout |
| `➜` (accent) | An action is starting | stdout |
| `⚠` (yellow) | Warning: the command keeps going | stderr |
| `✖` (red) | Error | stderr |
| `›` (dim) | Hint, usually a command to try next | stdout |
| `○` (dim) | Skipped, empty or off | stdout |
| `●` (green) | On / enabled | stdout |
| `◆` (accent) | dbtool brand / section title | stdout |
| `⠋⠙⠹…` | Running (spinner) | stdout, terminal only |

Errors usually come with a hint about what to do:

```
✖ Uploading to S3  AccessDenied: Access Denied  1.2s
  › Check your S3 settings with dbtool setting s3 test
```

---

## Prompts, pickers and forms

All prompts are built on [huh](https://github.com/charmbracelet/huh) and
render inline. Once you answer a prompt, it is replaced by a permanent
answer line:

```
✔ Name  prod
✔ Host  db.internal
✔ Use SSH?  yes
```

### Pickers (choose one / choose several)

| Key | Single picker | Multi picker |
|---|---|---|
| `↑` `↓` / `k` `j` | move | move |
| `/` | filter by typing | filter by typing |
| `space` / `x` | — | toggle the item |
| `ctrl+a` | — | select / deselect all |
| `enter` | choose | confirm the selection |
| `esc` | clear the filter | clear the filter |
| `ctrl+c` | cancel | cancel |

Long lists (more than 12 items) scroll; type `/` and a few letters to
narrow them down. When editing an existing choice (for example ignored
schemas), the current selection starts already checked, so `enter` keeps
it.

A multi-select answer line shortens long selections to
`a, b, c, d +7 more`.

### Text inputs

When a field already has a value (editing a config, or a default like
retention `0`), it is shown dimmed inside the input. Press `enter` on the
empty input to keep it, or type a new value. Input is checked as you type,
and `enter` is blocked until the value is valid:

| Field | Rule |
|---|---|
| Name, Host, User | required |
| Port, SSH port | a number from 1 to 65535 |
| Retention days | a whole number, 0 or more (0 = keep forever) |
| Job schedule | a valid 5-field cron expression; the next run time is shown after you enter it |

### Confirmations

Yes/no questions show two buttons. Use `←` `→` (or `y` / `n`) and `enter`.
The default answer starts selected.

### Passwords

Password prompts echo `*` for each character and never show the value. They
are the same in a terminal and when piped.

### Forms are grouped and reviewed

`config add/edit/duplicate` and `job add/edit` split their questions into
sections and show a **Review** box before saving. Config forms use
*Connection*, *SSH tunnel* (only when SSH is on), *Options*, *Credentials*
and *Schemas & tables*. Job forms use *Job*, *Source*, *Destination* and
*Options*. Names must be unique, and can't contain `|`, which the storage
format uses as a separator. The review step looks like this:

```
╭────────────────────────────────────────╮
│  ◆ Review                              │
│                                        │
│    Name       prod                     │
│    Server     backup@db.internal:3306  │
│    Route      ssh ops@bastion          │
│    Retention  7 days                   │
│    Password   saved                    │
╰────────────────────────────────────────╯
┃ Save this config?
┃
┃    Yes     No
```

Answer **No** and nothing is written. Flag-driven (non-interactive) runs
never show the review step.

### Canceling

`ctrl+c` cancels a prompt. A canceled picker returns to the command, which
treats it as "nothing chosen". A canceled text input or confirmation stops
the command with exit status 130, the same as `ctrl+c` always did.

---

## Dump and restore progress

A dump prints a header, one line per stage, a live progress bar while
`mydumper` runs, and a summary box at the end:

```
◆ Dump · prod

  Source       backup@db.internal:3306
  Route        SSH tunnel via ops@bastion
  Destination  s3://backups/mysql
  Telegram     yes

✔ Preparing  mydumper 0.16.9  0.3s
⠹ Dumping prod  ████████████░░░░░░░░  61%  61/100 tables · 37s · ETA 23s
```

When it finishes, each stage leaves a result line:

```
✔ Preparing  mydumper 0.16.9  0.3s
✔ Dumped 100 tables  1m02s
✔ Verifying dump  0.2s
✔ Saved  ~/dbtool-backups/prod_2026-11-08_020000  (412.3 MB)
✔ Uploading to S3  s3://backups/mysql/prod_2026-11-08_020000/  18.4s
  • sent prod_2026-11-08_020000.tar.part001 (1/9, 49.0 MB)
  …
✔ Sending to Telegram  41.0s

╭──────────────────────────────────────────────────────────╮
│  ✔ Dump complete                                          │
│                                                           │
│  Config    prod                                           │
│  Location  s3://backups/mysql/prod_2026-11-08_020000/     │
│  Tables    100                                            │
│  Size      412.3 MB                                       │
│  Took      2m04s                                          │
╰──────────────────────────────────────────────────────────╯
```

- **Progress bar.** It shows the percentage, table count, elapsed time and
  an ETA. If the table count can't be found in advance, it starts as a
  spinner with a running count. It turns into a real bar when `mydumper`
  reports a total.
- **Warnings don't hide the bar.** Messages printed during a stage (a
  rotated-out old dump, a Telegram chunk being sent) scroll above the live
  line.
- **Possible data loss.** If the dump-validation check fires, it is shown in
  a yellow box listing the affected tables and columns, and the summary
  title becomes *Dump complete with warnings*.
- **Failures** end the stage with `✖`, print the reason, and usually a hint.
  For example, `dbtool log --level debug` shows mydumper's full output.

Restores look the same: a `◆ Restore · name` header (source directory,
target, route, and whether existing tables are overwritten), then
*Preparing*, *Checking dump*, one progress bar per database, a
`✔ Restored <db>  N tables` line for each, and a *Restore complete* box.
If the destination goes read-only mid-restore, the retry shows as a
warning, for example
`⚠ Destination went read-only mid-restore — retrying in 10s (attempt 2 of 5)`.

**In cron, CI or a pipe**, nothing animates. You get only the plain result
lines (`✔ Dumped 100 tables  1m02s`), one per stage, so logs and cron mail
stay readable.

---

## Lists and overviews

`dbtool config list`:

```
  NAME      HOST               ROUTE             RETENTION   FLAGS
──────────────────────────────────────────────────────────────────────────────────────────
  prod      db.internal:3306   ssh ops@bastion   7 days      password saved
  staging   10.0.0.5:3306      direct            forever     no-locks · ignores 1 schema/2 tables

  • staging ignores schemas: tmp; tables: shop.sessions, shop.cache

  › 2 configs · manage them with dbtool config add | edit | delete
```

`dbtool job list`:

```
  NAME      TYPE      FLOW             SCHEDULE         NEXT RUN
───────────────────────────────────────────────────────────────────────────────
  nightly   dump      prod             0 2 * * *        in 3h27m (Mon 02:00)
  refresh   sync      prod → staging   */15 * * * 1-5   in 12m (Mon 00:00)
  weekly    restore   prod → staging   0 3 * * 0        in 6d (Sun 03:00)

  › 3 jobs · manage them with dbtool job add | edit | delete
```

Next-run times read `in 12m (Mon 23:00)`, `in 3h12m (Tue 02:00)`,
`in 1d3h (Wed 02:00)`, or `in 21d (Dec 1 02:00)` for runs more than a week
away.

`dbtool setting` (no subcommand) shows every setting in one place: work
directory, storage, encryption, proxy, Telegram and master password status.
Secrets are never shown.

When there is nothing to list you get a pointer instead of an empty table:

```
○ No configs yet
  › Create one with dbtool config add
```

`dbtool log` colors the level tags (`[ERROR]` red, `[WARN]` yellow, `[DEBUG]`
dimmed). Piped output stays raw.

---

## The dashboard (`dbtool ui`)

`dbtool ui` (alias `dbtool dashboard`) opens an optional full-screen view of
everything dbtool knows about:

```
 ◆ dbtool   1 Configs   2 Jobs   3 Settings   4 Logs                    v1.4.0
───────────────────────────────────────────────────────────────────────────────
  ❯ prod ssh                 │ prod
    staging                  │
                             │   Host       db.internal:3306
                             │   User       backup
                             │   Route      SSH tunnel via ops@bastion:22
                             │   Retention  7 days
                             │   Password   saved (encrypted)
                             │   Telegram   ● on
───────────────────────────────────────────────────────────────────────────────
 ✔ Dump of prod finished
 ↑/k up • ↓/j down • d dump • r restore • a add • e edit • c duplicate • x delete • tab next tab • q quit
```

| Tab | Shows | Keys |
|---|---|---|
| **1 Configs** | Config list and details of the selected config | `d`/`enter` dump · `r` restore · `a` add · `e` edit · `c` duplicate · `x` delete |
| **2 Jobs** | Job list with type, flow, schedule and next run time | `a` add · `e` edit · `x` delete |
| **3 Settings** | Read-only overview of all settings | `ctrl+r` reload |
| **4 Logs** | The last 1,000 log lines, colored by level, refreshed every 2s | `↑`/`↓` scroll · `g`/`G` top/bottom · `f` toggle follow |

Global keys: `tab` / `shift+tab` (or `←` `→`, `h` `l`) and `1`–`4` switch
tabs, `ctrl+r` reloads, and `q` / `ctrl+c` quits.

**How actions work.** The dashboard does not reimplement any command. Each
action temporarily leaves the dashboard and runs the normal dbtool command
(`dbtool dump --name prod`, `dbtool config add`, …) in the same terminal, so
you get the usual forms, password prompts, progress bars and summary. When
the command ends, press `enter` to return. The dashboard then reloads its
data and shows whether the command succeeded. Delete asks `y / n` in the
dashboard's status line before it runs anything.

The dashboard needs an interactive terminal. It refuses to start when
piped.

---

## Colors, icons and scripting

| Setting | Effect |
|---|---|
| `--no-color` (global flag) | No colors or text styling for this run |
| `NO_COLOR=1` | Same, for every run ([no-color.org](https://no-color.org)) |
| `DBTOOL_COLOR=always` | Force colors even when output is piped (e.g. into `less -R`) |
| `DBTOOL_COLOR=never` | Never use colors |
| `DBTOOL_ASCII=1` | Use ASCII icons (`[ok]`, `[x]`, `[!]`, `->`, `#` bars) instead of Unicode symbols |
| `TERM=dumb` | Implies ASCII icons |

Colors also turn off automatically when stdout is not a terminal. The palette
adapts to light and dark terminal themes.

**For scripts**, nothing changes:

- Every interactive command still accepts its flags (`--name`, `--pass`,
  `--dir`, …) and runs without prompts when given them.
- Without a terminal, pickers fall back to a numbered list
  (`Select: 2`, or `1,3` for multi-select), confirmations to `(y/N)`, and
  text inputs to `Label [current]:`. Piped input is read exactly as before.
- Errors and warnings go to stderr. Exit codes are unchanged (1 on failure,
  130 when a prompt is canceled with `ctrl+c`).
- Output meant to be parsed stays plain. `dbtool setting workdir get`
  prints only the path, and `dbtool log` prints raw lines when piped.

---

## Troubleshooting

**I see `?` boxes or broken symbols.** Your terminal font lacks those
glyphs. Set `DBTOOL_ASCII=1`, or use a font with Unicode symbol coverage
(most modern fonts have it).

**Colors look wrong on a light background.** The palette picks light or dark
shades by asking the terminal for its background color. Some terminals and
multiplexers don't answer. Set `COLORFGBG` (e.g. `COLORFGBG=0;15` for a
light background) or use `--no-color`.

**The picker doesn't respond to arrow keys over SSH or in an IDE terminal.**
Make sure a real TTY is allocated (`ssh -t`). Without one, dbtool switches
to the numbered fallback, which you can always use.

**I miss fzf.** The built-in picker covers what dbtool used fzf for (filter
by typing, multi-select, pre-selected items) and needs no install. You can
uninstall `fzf` if nothing else uses it.
