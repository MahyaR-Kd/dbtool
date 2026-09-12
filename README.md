# dbtool

A command-line tool for MySQL database backup and restore with SSH tunnelling support, scheduled jobs, and optional S3 storage.

## Features

- **Dump & restore** MySQL databases using [mydumper](https://github.com/mydumper/mydumper) / [myloader](https://github.com/mydumper/mydumper), with automatic compatibility handling across `mydumper` 0.x and 1.x's CLI differences
- **SSH tunnelling** and/or a **SOCKS5 proxy** — connect to databases through a bastion host, a jump proxy, or both chained together
- **Named connection configs** — save and reuse database connection details, including (optionally) the DB password itself, protected by a master password
- **Scheduled jobs** — define dump, restore, or sync (dump + restore) jobs with cron expressions, run unattended via crontab
- **Local or S3 storage** — dump output can go to a local directory or an S3 / S3-compatible bucket (MinIO, R2, Spaces, etc.)
- **Telegram delivery** — optionally send every dump to a Telegram chat, auto-chunked to fit the Bot API's file-size limit
- **Dump validation safety net** — flags a known class of `mydumper` bug where a column's real value never makes it into the dump at all
- **Encrypted credentials** — every password/key/token dbtool stores is encrypted at rest, never in plaintext
- **Arrow-key picker** for every "pick one from a list" prompt, with automatic fallback to a plain numbered list for scripts
- **Configurable output directory** for dump files
- **Shell completion** for bash, zsh, and fish (installed automatically)

## Prerequisites

| Tool | Required for |
|------|-------------|
| `mydumper` | `dump` command |
| `myloader` | `restore` command |

dbtool will detect missing tools and offer to install them automatically on Ubuntu/Debian, CentOS/RHEL/Fedora, and macOS (Homebrew). Prefer installing `mydumper`/`myloader` from [the project's own releases](https://github.com/mydumper/mydumper/releases) or its official apt repo over your OS's default package repo — distro repos commonly lag upstream by years, and dbtool's compatibility handling (see [Architecture](#architecture)) only smooths over *known* differences between the two CLI generations, not undiscovered bugs in a very old build.

## Installation

### Download a pre-built binary

Pick the binary for your platform from the [latest release](https://github.com/MahyaR-Kd/dbtool/releases/latest):

| Platform | Architecture | Download |
|----------|-------------|---------|
| Linux    | amd64       | [dbtool-linux-amd64](https://github.com/MahyaR-Kd/dbtool/releases/latest/download/dbtool-linux-amd64) |
| Linux    | arm64       | [dbtool-linux-arm64](https://github.com/MahyaR-Kd/dbtool/releases/latest/download/dbtool-linux-arm64) |
| macOS    | amd64 (Intel) | [dbtool-darwin-amd64](https://github.com/MahyaR-Kd/dbtool/releases/latest/download/dbtool-darwin-amd64) |
| macOS    | arm64 (Apple Silicon) | [dbtool-darwin-arm64](https://github.com/MahyaR-Kd/dbtool/releases/latest/download/dbtool-darwin-arm64) |
| Windows  | amd64       | [dbtool-windows-amd64.exe](https://github.com/MahyaR-Kd/dbtool/releases/latest/download/dbtool-windows-amd64.exe) |
| Windows  | arm64       | [dbtool-windows-arm64.exe](https://github.com/MahyaR-Kd/dbtool/releases/latest/download/dbtool-windows-arm64.exe) |

**Quick install on Linux / macOS:**

```bash
# Linux amd64
curl -L https://github.com/MahyaR-Kd/dbtool/releases/latest/download/dbtool-linux-amd64 -o dbtool
chmod +x dbtool

# macOS Apple Silicon
curl -L https://github.com/MahyaR-Kd/dbtool/releases/latest/download/dbtool-darwin-arm64 -o dbtool
chmod +x dbtool

# Then let dbtool install itself to your PATH
./dbtool install
```

### Build from source

```bash
git clone https://github.com/MahyaR-Kd/dbtool.git
cd dbtool
make build
./dbtool install
```

`make build` compiles the binary. `dbtool install` copies it to `/usr/local/bin` (or `~/bin` if `/usr/local/bin` is not writable) and sets up shell completion.

### go install

```bash
go install github.com/MahyaR-Kd/dbtool@latest
```

## Usage

Every "pick one from a list" prompt (selecting a config, a job, a dump
directory, which schemas/tables to include or ignore, ...) opens an
fzf-like picker: arrow keys (or type to fuzzy-filter) to navigate, Tab to
toggle a selection where multiple picks are allowed, Enter to confirm, Esc
to cancel. It's built into the `dbtool` binary — no separate `fzf` install
needed. When stdin isn't an interactive terminal (piping input from a
script), it automatically falls back to a compact numbered list — type the
number (or, for a multi-select prompt, comma-separated numbers) and press
Enter — so scripted/automated use is unaffected.

One cosmetic note: the picker library always renders using the *entire*
current terminal height, with no option to constrain it — on a tall
terminal window, a short list (a couple of configs, say) ends up with a
large empty gap above it. That's a limitation of the underlying library,
not a dbtool bug — two upstream feature requests ask for a height option
([#261](https://github.com/ktr0731/go-fuzzyfinder/issues/261),
[#134](https://github.com/ktr0731/go-fuzzyfinder/issues/134)), both open.
Selection, arrow keys, and fuzzy search all work correctly regardless.

### Manage connection configs

```bash
# Add a new connection config (interactive)
dbtool config add

# Add a new connection config (non-interactive)
dbtool config add --name prod --host 10.0.0.1 --port 3306 --user root

# List saved configs
dbtool config list

# Delete a config (interactive)
dbtool config delete

# Delete a config (non-interactive)
dbtool config delete --name prod

# Edit an existing config (interactive)
dbtool config edit

# Edit an existing config (non-interactive)
dbtool config edit --name prod --host 10.0.0.2 --retention-days 7

# Save this config's DB password so you're not asked for it on every dump/restore
dbtool config add --name prod --host 10.0.0.1 --port 3306 --user root --password secret
dbtool config edit --name prod --password newsecret   # change it
dbtool config edit --name prod --password ""           # clear it
```

A config stores: name, host, port, user, optional SSH tunnel settings, schemas/tables to ignore during dumps, dump retention days (`0` = keep forever), an optional `--no-locks` flag (below), and — if you choose to save one — a DB password.

When adding or editing a config interactively, dbtool connects to the database to list its schemas and asks how you want to choose which ones get dumped:

- **Ignore mode** (default) — pick the few schemas/tables you *don't* want, dump everything else.
- **Include mode** — pick the few schemas/tables you *do* want, ignore everything else. Handy when a database has many schemas and you only care about a couple — the list is converted to an ignore list under the hood, so the stored format never changes; a schema added to the database later shows up as included by default until you re-run `config edit` to refresh the selection.

`--no-locks`: some MySQL users lack the privilege `mydumper`'s default consistency locking needs (`RELOAD` on older `mydumper`, `BACKUP_ADMIN` on 1.0+, for its added `LOCK INSTANCE FOR BACKUP` step). Setting this on a config skips that locking entirely — you lose the guarantee that every table reflects exactly the same instant if there's concurrent write traffic, but each InnoDB table is still individually consistent via `mydumper`'s per-thread transaction snapshot.

#### Saved DB passwords and the master password

Saving a config's DB password (via `--password`, or answering "yes" when
`config add`/`config edit` asks) protects it with a **master password** —
separate from everything else dbtool encrypts. The first time you save a
config password, dbtool asks you to create this master password; after
that, using a saved password (running `dbtool dump`/`dbtool restore`
without `--pass`) asks for the master password once. Within a single
command run, it's only ever asked once, even if more than one credential
needs decrypting.

This master password is **never stored anywhere** — only a small
verification record is, so a wrong guess can be detected without dbtool
ever writing the real password to disk. That also means if you forget it,
any config passwords saved under it are gone for good — that's what
encryption means. `dbtool setting master-password reset` is the recovery
path for that case: it clears the verification record and every saved
config password so you can set a new master password and re-save them.

If you just want a *different* master password — you haven't forgotten the
current one, you just want to change it — use `dbtool setting
master-password change` instead: it verifies your current master password,
then re-encrypts every saved config password under a new one, so nothing
gets cleared and you don't have to re-enter and re-save anything.

This only affects DB config passwords used for a `dump`/`restore` you run
yourself. Scheduled/cron jobs are completely unaffected — job passwords,
S3 keys, the proxy password, and the Telegram bot token keep using their
own auto-generated key with no password prompt, so background jobs never
need a human present to type anything.

```bash
# Check whether a master password is set up
dbtool setting master-password status

# Change it — re-encrypts saved config passwords under the new one, nothing is cleared
dbtool setting master-password change

# Forget it and clear every saved config password (asks to confirm)
dbtool setting master-password reset
```

### Dump a database

```bash
# Interactive config selection
dbtool dump

# Non-interactive (use saved config by name)
dbtool dump --name my-config

# Pass the database password on the command line
dbtool dump --name my-config --pass secret
```

After `mydumper` finishes, dbtool checks each table's data file for columns
that exist in its schema but are missing from the `INSERT` statements
themselves (excluding real `GENERATED ALWAYS AS (...)` columns, which are
legitimately omitted). If any are found, it prints a loud warning — this
catches a known issue in older `mydumper` versions where a column with an
automatic `DEFAULT`/`ON UPDATE CURRENT_TIMESTAMP` gets misidentified as a
true generated column and silently dropped from the dump, meaning its real
value is never captured and gets replaced by the column's default when
restored. The warning doesn't fail the dump (so scheduled jobs keep running)
— if it fires, upgrade `mydumper`/`myloader` (not via your OS package
manager, which usually lags upstream by years) and take a fresh dump.

### Restore a database

```bash
# Fully non-interactive
dbtool restore --name my-config --dir /path/to/dump-dir

# Mixed flow: interactive config selection, non-interactive dir
dbtool restore --dir /path/to/dump-dir

# Fully interactive — pick a config, then a dump (from the local work
# directory, or from S3 if that's the active storage backend)
dbtool restore
```

The same validation check dump runs also runs here, right before `myloader`
loads the data. This matters even if the dump itself was clean when it was
taken — a dump you downloaded from S3, one taken a long time ago, or one
made before you last upgraded `mydumper` never went through the dump-time
check with your current tooling. Catching it here, before the data actually
lands, is the more important of the two spots this check runs in.

### Check a dump directory directly

```bash
dbtool validate --dir /root/dbtool-backups/mydb_2026-09-10_132320
```

Runs the same column-validation check dump/restore already run automatically, against any dump directory — useful for spot-checking a dump you didn't just take (downloaded from S3, sitting on disk from a while ago) without doing a full restore first. Unlike the automatic checks, this one exits non-zero when it finds a problem, so it can be used as a gate in a script.

### Manage scheduled jobs

```bash
# Add a new job (interactive)
dbtool job add

# Add a new job (non-interactive)
dbtool job add --name nightly --type dump --schedule "0 2 * * *" --src-config prod --src-pass secret

# List jobs
dbtool job list

# Delete a job (interactive)
dbtool job delete

# Delete a job (non-interactive)
dbtool job delete --name nightly

# Edit a job (interactive)
dbtool job edit

# Edit a job (non-interactive)
dbtool job edit --name nightly --schedule "0 3 * * *"
```

Job types:

| Type | Description |
|------|-------------|
| `dump` | Run a dump on a schedule |
| `restore` | Run a restore on a schedule |
| `sync` | Dump from source and immediately restore to destination |

Schedules use standard 5-field cron syntax (e.g. `0 2 * * *` for every day at 02:00).

### Background scheduling

```bash
# Enable background scheduling (installs a crontab entry)
dbtool schedule enable

# Disable background scheduling (removes the crontab entry)
dbtool schedule disable
```

When enabled, dbtool adds a `* * * * *` crontab entry that checks for due jobs every minute.

### Configure dump output directory

```bash
# Print the current work directory
dbtool workdir get

# Set a custom work directory
dbtool workdir set /data/backups

# Reset to the default (~/dbtool-backups)
dbtool workdir set --default
```

### Store dumps in S3 (or an S3-compatible bucket)

```bash
# Switch to S3 storage (non-interactive)
dbtool setting s3 config --storage-type s3 \
  --bucket my-backups --region us-east-1 \
  --access-key AKIA... --secret-key ...

# S3-compatible endpoint (MinIO, R2, Spaces, ...)
dbtool setting s3 config --storage-type s3 \
  --bucket my-backups --region auto \
  --access-key ... --secret-key ... \
  --endpoint https://minio.example.com

# Optional key prefix within the bucket
dbtool setting s3 config --storage-type s3 --bucket my-backups --region us-east-1 \
  --access-key ... --secret-key ... --prefix backups/prod

# Switch back to local storage
dbtool setting s3 config --storage-type local

# Show the current storage configuration (keys are masked)
dbtool setting s3 show
```

`mydumper` always writes to the local work directory first (it's a local
tool — it can't write directly to S3), then dbtool uploads the result and
deletes the local copy once the upload succeeds. If the upload fails, the
local copy is kept so nothing is lost. `dbtool restore` (with S3 storage
active and no `--dir`) lists available dumps in the bucket, downloads the
one you pick to a temp directory, restores from it, then cleans the temp
directory up. Picking `--region`: real AWS S3 needs the bucket's actual
region; most S3-compatible services just need *some* non-empty string
(Cloudflare R2 uses `auto`).

### Route connections through a SOCKS5 proxy

```bash
dbtool setting proxy set --host 127.0.0.1 --port 1080
dbtool setting proxy set --host 127.0.0.1 --port 1080 --user myuser --password secret

dbtool setting proxy show
dbtool setting proxy clear
```

When configured, dump/restore connections route through this proxy instead of connecting directly. It composes with a config's own SSH tunnel — if a config has SSH enabled, the tunnel's SSH connection itself goes through the proxy, and the DB connection then goes through the SSH tunnel.

### Deliver dumps to Telegram

```bash
# Configure the bot (interactive)
dbtool setting telegram set

# Configure the bot (non-interactive)
dbtool setting telegram set --token 123456:ABC-DEF --chat-id 987654321

# Optional: override the default 49 MB chunk size
dbtool setting telegram set --token 123456:ABC-DEF --chat-id 987654321 --chunk-size-mb 40

# Verify the bot token and chat ID work
dbtool setting telegram test

# Show / remove the configuration
dbtool setting telegram show
dbtool setting telegram clear
```

When a Telegram bot is configured, every dump is bundled into a tar archive
and split into chunks under the configured size (49 MB by default, matching
the standard Telegram Bot API's per-file limit) after it has been saved
locally or uploaded to S3 — whichever storage backend is active. Each chunk
is sent as a document to the configured chat, preceded by a message with the
dump name, total size, part count, and the command to reassemble them:

```bash
cat <dump-name>.tar.part* > <dump-name>.tar && tar -xf <dump-name>.tar
```

Telegram delivery is a supplementary channel: if it fails (bad token, network
error, rate limit), dbtool logs a warning and continues — it never fails a
dump that already succeeded locally or on S3. It applies to `dbtool dump` and
to scheduled `dump`/`sync` jobs; `restore` never sends to Telegram.

### Logs

```bash
# Print all log entries
dbtool log

# Only WARN and above
dbtool log --level warn

# Follow, like tail -f
dbtool log -f

# Follow, filtered
dbtool log -f --level info
```

Every dump/restore logs which `mydumper`/`myloader` build actually ran it (`using /usr/local/bin/mydumper (mydumper v1.0.5-1, ...)`, at `INFO`), which is useful for tracing whether an older dump might be affected by a version-specific tool bug after the fact.

### Other commands

```bash
# Show the current version
dbtool --version
```

## Data files

All dbtool data is stored under `~/.dbtool/`:

| File | Contents |
|------|----------|
| `dbtool.conf` | Database connection configs (saved passwords encrypted, see below) |
| `dbtool.jobs` | Scheduled job definitions (credentials encrypted, see below) |
| `dbtool.settings` | Settings — work directory, S3/proxy/Telegram config (credentials encrypted, see below) |
| `dbtool.key` | Local encryption key for job/settings credentials (0600, generated on first use) |
| `dbtool.masterkey` | Verification record for the master password protecting saved config DB passwords (0600, generated the first time you save one — never contains the password itself) |
| `dbtool.log` | Application log |

Dump output is written to `~/dbtool-backups/<config-name>_<timestamp>/` by default.

## Credential security

Any credential dbtool has to persist to disk — scheduled job DB passwords,
S3 access/secret keys, the SOCKS5 proxy password, and the Telegram bot token
— is encrypted at rest with AES-256-GCM before being written to
`dbtool.jobs` / `dbtool.settings`. The encryption key lives separately in
`dbtool.key` (0600, generated automatically the first time it's needed), and
both credential files are also 0600 (owner-only).

This is a meaningful improvement over plaintext, but it has an honest limit:
scheduled jobs must be able to decrypt unattended (cron runs with nobody
present to type a passphrase), so the key has to live on the same machine as
the encrypted data — it does not protect against an attacker who already has
full read access to your account. What it does protect against is the much
more common accidental-leak path: a config file swept into a backup,
committed to git, or pasted into a support bundle no longer hands over
credentials in plaintext by itself.

A saved connection-config DB password uses a *different* scheme — a
user-chosen master password rather than the auto-generated key — precisely
because it isn't needed by unattended jobs. See [Saved DB passwords and the
master password](#saved-db-passwords-and-the-master-password) above.

**Upgrading from an older version**: if you already have plaintext
credentials in `dbtool.jobs` or `dbtool.settings` from before this was added,
they're migrated automatically and transparently — the next time dbtool
reads either file, it re-saves it encrypted. There's nothing to run by hand.

### Passwords are never shown while typing

Every interactive password prompt (DB passwords, job passwords, S3 keys, the
proxy password, the Telegram bot token, the master password) masks input
with `*` per character instead of showing it in plain text.

### Avoiding passwords in shell history / `ps`

Flags like `--pass`, `--src-pass`, `--dst-pass`, `--password`, `--secret-key`,
and `--token` still work for scripted use, but a value passed on the command
line is visible in shell history and to anyone running `ps` on the box while
the command executes. To avoid that, every one of these flags falls back, in
order, to:

1. The flag value, if given (existing scripts keep working unchanged).
2. An environment variable, if set:

   | Flag | Env var |
   |------|---------|
   | `dump`/`restore` `--pass` | `DBTOOL_DB_PASS` |
   | `job add`/`job edit` `--src-pass` | `DBTOOL_SRC_PASS` |
   | `job add`/`job edit` `--dst-pass` | `DBTOOL_DST_PASS` |
   | `setting telegram set` `--token` | `DBTOOL_TELEGRAM_TOKEN` |

3. A masked interactive prompt.

So `dbtool job add --name nightly --type dump --schedule "0 2 * * *" --src-config prod`
(no `--src-pass`) now prompts for the password instead of erroring out.

## Architecture

dbtool is a thin orchestration layer, not a reimplementation of MySQL
dump/restore logic. `dump`/`restore` shell out to `mydumper`/`myloader` as
external processes; dbtool's own code handles everything around them —
connection configs, SSH/SOCKS5 tunnelling, scheduling, storage backends,
credential encryption, and a safety-net validation pass on the output.

### Command layer (`cmd/`)

Built on [Cobra](https://github.com/spf13/cobra). Each file registers one
command (or a small related group) via `init()`. Most commands support both
an interactive flow (prompts / the arrow-key picker) and a fully-flagged
non-interactive flow for scripting — the same command handles both,
branching on whether any flags were set.

One structural quirk worth knowing: [cmd/root.go](cmd/root.go) computes
`installedInPath` — whether the currently running binary is the one
resolved by `$PATH`. Every command except `install` is gated on it, so a
freshly built, not-yet-installed binary only exposes `dbtool install`,
preventing accidental use of an unmanaged copy against real data.

### Package layout (`internal/`)

| Package | Responsibility |
|---|---|
| `types` | The shared `Config` struct (DB connection + SSH + ignore rules + retention + saved password) |
| `config` | Connection config storage (`dbtool.conf`) and interactive prompts, including live schema/table fetching over a real MySQL connection |
| `job` | Scheduled job storage (`dbtool.jobs`), cron-field matching, and `RunJob`/`RunScheduler` |
| `db` | The dump/restore engine: shells out to `mydumper`/`myloader`, patches known-bad schema defaults, detects the installed tool version to pick compatible CLI flags, and validates dump output for silently-dropped columns |
| `connection` | Composes an SSH tunnel and/or a SOCKS5 proxy into a local forwarding endpoint before a DB connection |
| `ssh` / `proxy` | The two tunnelling mechanisms `connection` composes: shelling out to the system `ssh` client, and a from-scratch SOCKS5 client |
| `s3store` | Upload/list/download/delete against S3 or an S3-compatible endpoint |
| `rotate` | Deletes dump directories (local or S3) past their configured retention |
| `telegram` | Bundles a dump into a tar archive, splits it into Telegram-file-size chunks, and delivers them via the Bot API |
| `settings` | JSON-backed settings (`dbtool.settings`): work directory, storage backend, proxy, Telegram |
| `secret` | AES-256-GCM encryption with a local auto-generated key — protects job passwords, S3 keys, the proxy password, and the Telegram token; no human interaction needed, so scheduled jobs keep working unattended |
| `credvault` | A second, separate encryption scheme gated by a user-chosen master password — protects only the optional DB password saved on a connection config, since that's only ever needed for a dump/restore a human runs directly |
| `secureinput` | Masked (`*`-echoing) password prompts, with an env-var/flag resolution cascade and a plain-line fallback when stdin isn't a terminal |
| `interactivelist` | The fzf-like arrow-key picker (wraps `go-fuzzyfinder`), with the same non-terminal fallback to a numbered list |
| `progress` | Parses `mydumper`/`myloader`'s verbose stderr output to drive a real progress bar instead of a spinner |
| `deps` | Detects missing `mydumper`/`myloader`/`mysql` and offers to install them via the OS package manager |
| `logger` | Leveled file logging to `dbtool.log` |
| `paths` | Resolves and creates `~/.dbtool/` |

### Data flow

**Dump**: [cmd/dump.go](cmd/dump.go) resolves a config (flag, saved
password, or interactive picker) → [`db.RunDump`](internal/db/dump.go) sets
up any SSH/proxy tunnel, shells out to `mydumper` with an exclusion regex
built from `IgnoredSchemas`/`IgnoredTables`, patches the resulting schema
files for known-bad defaults, runs the column-validation safety check,
rotates old dumps past their retention, then optionally uploads to S3
and/or delivers to Telegram.

**Restore**: [cmd/restore.go](cmd/restore.go) resolves a config and a dump
directory (local listing or S3 listing) →
[`db.RunRestore`](internal/db/restore.go) re-runs the same schema patch and
validation check — this dump may never have gone through `RunDump` at all
(downloaded from S3, or taken by an older dbtool) — then shells out to
`myloader`.

**Scheduled jobs**: a `* * * * *` crontab entry (installed by `dbtool
schedule enable`) invokes the hidden `_scheduler_tick` command every
minute; it loads `dbtool.jobs`, matches each job's cron expression against
the current time, and calls [`job.RunJob`](internal/job/cron.go) for any
that are due — which calls the same `db.RunDump`/`db.RunRestore` the CLI
commands use, just with the job's own stored (auto-generated-key-encrypted)
credentials rather than a config's master-password-protected one. This is
also why scheduled jobs are entirely unaffected by the master password:
`RunDump`/`RunRestore`/`RunJob` never reference a config's `Password`
field at all — only `config.AskPassword` (called from the CLI `dump`/
`restore` commands) does.

### `mydumper`/`myloader` version compatibility

`mydumper` had breaking CLI changes going from the 0.x to 1.x line —
notably `--no-locks` was removed in favor of `--sync-thread-lock-mode
NO_LOCK`, and myloader's `--overwrite-tables` was removed in favor of
`--drop-table` (verified directly against myloader's own argument-parsing
source, not inferred from docs). [`internal/db/toolversion.go`](internal/db/toolversion.go)
runs `<tool> --version` before each dump/restore, parses the version, and
picks the CLI syntax that actually exists in the installed build — so the
same dbtool binary works correctly against either tool generation without
configuration.

### Dump validation safety net

[`internal/db/validate.go`](internal/db/validate.go) compares each table's
schema (the full column list, from `-schema.sql`) against its data file's
`INSERT` statement column list. A column present in the schema but missing
from the `INSERT` (and not a true `GENERATED ALWAYS AS (...)` column) means
its real value was never captured — restoring will silently apply the
column's `DEFAULT` instead. This runs automatically after every dump and
before every restore, and is also exposed directly via `dbtool validate`.

## Building

```bash
# Build binary (version taken from git tags)
make build

# Install via go install
make install

# Remove the compiled binary
make clean
```

## Testing

```bash
go test ./...
```

Tests favor small, pure functions extracted specifically to be testable —
e.g. the `mydumper --regex` exclusion builder, the dump-validation column
comparison, and the `secureinput`/`interactivelist` fallback parsers are
all tested directly against literal strings or fake readers rather than
through the CLI. Anything that would otherwise need a real terminal, the
real `~/.dbtool` directory, or a real password prompt is faked or isolated
instead:

- `secureinput`/`credvault` tests swap in a canned password-reader function in place of a real terminal read.
- Tests that touch `~/.dbtool` (`secret`, `credvault`, `db`'s tool-version logging) redirect `HOME`/`USERPROFILE` to a `t.TempDir()` so they never touch the real directory.
- [internal/db/validate_test.go](internal/db/validate_test.go) and [dump_regex_test.go](internal/db/dump_regex_test.go) replay real bug reports — a literal schema and `INSERT` statement from an actual `mydumper` issue — as regression tests, rather than synthetic examples.

## License

MIT — see [LICENSE](LICENSE) for details.
