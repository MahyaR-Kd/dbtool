package db

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"dbtool/internal/logger"
)

// toolVersion is a parsed mydumper/myloader --version result. OK is false
// when the version could not be determined or parsed — callers must treat
// that as "unknown," not as any particular version.
type toolVersion struct {
	Major, Minor, Patch int
	Raw                 string
	OK                  bool
}

// v? handles both "mydumper 0.10.0, ..." (older builds) and
// "mydumper v1.0.5-1, ..." (current release packaging, which prefixes the
// version with a literal "v") — the trailing "-1" packaging suffix doesn't
// need to be matched since the three \d+ groups stop at the patch number.
var (
	mydumperVersionRe = regexp.MustCompile(`(?i)mydumper\s+v?(\d+)\.(\d+)\.(\d+)`)
	myloaderVersionRe = regexp.MustCompile(`(?i)myloader\s+v?(\d+)\.(\d+)\.(\d+)`)
)

// detectMydumperVersion runs "<path> --version", logs the raw result
// (giving every dump a durable, per-run audit trail of exactly which
// mydumper build made it — see ValidateDump for why that matters), and
// returns a parsed version.
//
// This exists for more than logging: mydumper's command-line surface has
// had breaking changes across major versions — most notably, --no-locks was
// removed entirely in mydumper 1.0 in favor of --sync-thread-lock-mode
// (see lockModeArgs) — so dbtool needs to know which release it's talking
// to in order to build a working argument list.
func detectMydumperVersion(path string) toolVersion {
	return detectToolVersion(path, mydumperVersionRe)
}

// detectMyloaderVersion is the myloader equivalent of detectMydumperVersion.
// dbtool doesn't currently branch myloader's arguments on version, but this
// keeps the same audit-trail logging in place for it.
func detectMyloaderVersion(path string) toolVersion {
	return detectToolVersion(path, myloaderVersionRe)
}

func detectToolVersion(path string, versionRe *regexp.Regexp) toolVersion {
	out, err := exec.Command(path, "--version").CombinedOutput() // #nosec G204 -- path comes from findExecutable, which only resolves via PATH lookup or a fixed allowlist of system directories, never user input
	if err != nil {
		logger.Debug("could not determine version for %s: %v", path, err)
		return toolVersion{}
	}

	firstLine := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	logger.Info("using %s (%s)", path, firstLine)

	return parseToolVersion(firstLine, versionRe)
}

// parseToolVersion is split out from detectToolVersion so the parsing logic
// can be unit tested against literal --version strings without needing a
// real (or fake) executable on disk.
func parseToolVersion(versionLine string, versionRe *regexp.Regexp) toolVersion {
	m := versionRe.FindStringSubmatch(versionLine)
	if m == nil {
		logger.Debug("could not parse a version number out of %q", versionLine)
		return toolVersion{Raw: versionLine}
	}

	major, errMajor := strconv.Atoi(m[1])
	minor, errMinor := strconv.Atoi(m[2])
	patch, errPatch := strconv.Atoi(m[3])
	if errMajor != nil || errMinor != nil || errPatch != nil {
		return toolVersion{Raw: versionLine}
	}

	return toolVersion{Major: major, Minor: minor, Patch: patch, Raw: versionLine, OK: true}
}

// lockModeArgs returns the mydumper argv fragment that requests a
// no-consistency-locking dump (dbtool's NoLocks config option — for DB
// users that lack the privilege mydumper's default locking needs: RELOAD
// for the pre-1.0 FLUSH TABLES WITH READ LOCK, or BACKUP_ADMIN for 1.0+'s
// added LOCK INSTANCE FOR BACKUP DDL lock).
//
// mydumper removed --no-locks in 1.0.0, replacing it with
// --sync-thread-lock-mode NO_LOCK. When the installed version can't be
// determined, this falls back to the pre-1.0 flag, matching dbtool's
// behavior before version detection existed.
func lockModeArgs(v toolVersion) []string {
	if v.OK && v.Major >= 1 {
		return []string{"--sync-thread-lock-mode", "NO_LOCK"}
	}
	return []string{"--no-locks"}
}

// overwriteTablesArgs returns the myloader argv fragment that makes a
// restore overwrite tables that already exist in the destination (dbtool's
// overwriteTables option).
//
// myloader removed the --overwrite-tables flag in 1.0.0. Verified directly
// against its current option parser (src/myloader/myloader_arguments.c):
// --drop-table (short form -o), passed with no value, defaults to DROP
// mode and internally sets the same overwrite_tables=TRUE the old flag did
// — it's the direct successor, not just an approximation. (myloader 1.0+
// also has --overwrite-unsafe, described as "the same but starts loading
// sooner, may cause InnoDB deadlocks for foreign keys" — deliberately not
// used here since dbtool has no way to know whether that tradeoff is safe
// for a given restore.)
func overwriteTablesArgs(v toolVersion) []string {
	if v.OK && v.Major >= 1 {
		return []string{"--drop-table"}
	}
	return []string{"--overwrite-tables"}
}
