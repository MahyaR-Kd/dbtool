package db

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"dbtool/internal/logger"
)

// removedSQLModeValues lists sql_mode values a newer MySQL server now
// hard-rejects if you try to SET them, even though an older MySQL or
// MariaDB source still reports them as active. mydumper copies the
// source's @@SQL_MODE verbatim into a "/*!40101 SET SQL_MODE=...*/;"
// preamble written to the top of every file it produces — schema-create,
// per-table schema, and data files alike (mydumper's own
// initialize_header_in_gstring builds this once and copies it into every
// new file) — so restoring a dump taken from such a source into a newer
// destination fails on the very first file myloader touches, before any
// table data is even loaded. NO_AUTO_CREATE_USER is the confirmed case:
// deprecated in MySQL 5.7, removed with a hard error ("ERROR 1231:
// Variable 'sql_mode' can't be set to the value of 'NO_AUTO_CREATE_USER'")
// in MySQL 8.0.
var removedSQLModeValues = []string{"NO_AUTO_CREATE_USER"}

// sqlModePreambleWindow bounds how many bytes of a file's decompressed
// content sanitizeSQLModePreamble inspects for the SET SQL_MODE line.
// mydumper's header block (SET NAMES, FOREIGN_KEY_CHECKS, SQL_MODE,
// TIME_ZONE) is always a few hundred bytes at most, comfortably inside
// this.
const sqlModePreambleWindow = 4096

var (
	// reNotNullZeroDate matches NOT NULL DEFAULT '0000-00-00[...]' and replaces
	// it with NULL DEFAULT NULL, making the column nullable and dropping the
	// invalid zero-date default.
	reNotNullZeroDate = regexp.MustCompile(`(?i)NOT NULL DEFAULT '0000-00-00[^']*'`)

	// reZeroDate matches DEFAULT '0000-00-00[...]' on columns that are already
	// nullable and replaces it with DEFAULT NULL.
	reZeroDate = regexp.MustCompile(`(?i)DEFAULT '0000-00-00[^']*'`)

	// reDateNotNullCurrentTs matches a bare `date` column (not datetime/timestamp)
	// with NOT NULL DEFAULT current_timestamp() and makes it nullable with NULL default,
	// because current_timestamp() is not a valid default for the date type.
	reDateNotNullCurrentTs = regexp.MustCompile(`(?i)(\bdate\b)\s+NOT\s+NULL\s+DEFAULT\s+current_timestamp\s*(?:\(\s*\))?`)

	// reDateCurrentTs matches a bare `date` column (not datetime/timestamp) that is
	// already nullable but has DEFAULT current_timestamp(), and replaces it with
	// DEFAULT NULL.
	reDateCurrentTs = regexp.MustCompile(`(?i)(\bdate\b)\s+DEFAULT\s+current_timestamp\s*(?:\(\s*\))?`)

	// reDoubleQuotedCreateTable identifies table-schema files written using
	// ANSI-style double-quoted identifiers. Only those files are eligible for
	// quote conversion, avoiding changes to ordinary SQL string literals.
	reDoubleQuotedCreateTable = regexp.MustCompile(`(?i)\bCREATE\s+(?:TEMPORARY\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?"`)
)

// PatchDumpDir rewrites every table-schema file in dir so that columns whose
// default value is an invalid zero date/datetime ('0000-00-00' or
// '0000-00-00 00:00:00') are fixed before myloader runs:
//
//   - NOT NULL columns are made nullable (NOT NULL → NULL) and the invalid
//     default is replaced with DEFAULT NULL.
//   - Columns that are already nullable have the invalid default replaced with
//     DEFAULT NULL.
//
// For a dump whose own metadata file shows no [config] quote-character
// key — meaning mydumper < 1.0 produced it — it also converts ANSI-style
// double-quoted identifiers to MySQL backticks, needed for myloader ≤
// 0.10, which always expects backticks and has no way to know the dump
// used a different convention.
//
// A dump with that metadata key (mydumper 1.x) must NOT go through this
// conversion: mydumper 1.x auto-detects ANSI_QUOTES mode on the source at
// dump time, uses that quoting consistently across every file in the
// dump, and records the choice in the metadata file for myloader 1.x to
// read back and enforce against every file. Converting a double-quoted
// file to backticks without also rewriting that recorded value creates a
// mismatch myloader can't recover from: it looks for whatever character
// metadata told it to expect, doesn't find it in a file dbtool silently
// rewrote, and aborts with "Identifier quote character (...) not found"
// (confirmed against mydumper's own source:
// src/mydumper/mydumper_start_dump.c's detect_quote_character() and its
// "[config]\nquote-character = %s\n" metadata write, and myloader's read
// of that same key in src/myloader/myloader_process.c). Checking the
// dump's own metadata file — rather than the currently installed
// mydumper's version — is what makes this correct regardless of when or
// on which machine a dump gets restored.
func PatchDumpDir(dir string) {
	sanitizeSQLModePreamble(dir)

	convertAnsiQuotes := !dumpMetadataHasQuoteCharacter(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		logger.Debug("patchDumpDir: cannot read dir %s: %v", dir, err)
		return
	}

	for _, e := range entries {
		if e.IsDir() || !isTableSchemaFile(e.Name()) {
			continue
		}
		fullPath := filepath.Join(dir, e.Name())
		if err := patchSchemaFile(fullPath, convertAnsiQuotes); err != nil {
			logger.Debug("patchDumpDir: failed to patch %s: %v", fullPath, err)
		}
	}
}

// sanitizeSQLModePreamble rewrites every SQL file in dir (schema-create,
// table schema, and data files alike) to strip any removedSQLModeValues
// entry from its "/*!40101 SET SQL_MODE=...*/;" preamble. Only files that
// actually need a change are touched — see patchSQLModeFile — and only
// the header portion of each is ever decoded into memory, so this stays
// cheap even for multi-gigabyte data files.
func sanitizeSQLModePreamble(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		logger.Debug("sanitizeSQLModePreamble: cannot read dir %s: %v", dir, err)
		return
	}

	for _, e := range entries {
		name := e.Name()
		isSQLFile := strings.HasSuffix(name, ".sql") || strings.HasSuffix(name, ".sql.gz") || strings.HasSuffix(name, ".sql.zst")
		if e.IsDir() || !isSQLFile {
			continue
		}
		fullPath := filepath.Join(dir, name)
		if err := patchSQLModeFile(fullPath); err != nil {
			logger.Debug("sanitizeSQLModePreamble: failed to patch %s: %v", fullPath, err)
		}
	}
}

// patchSQLModeFile strips any removedSQLModeValues entry from the
// SET SQL_MODE preamble in the first sqlModePreambleWindow bytes of
// path's decompressed content, streaming everything after that unchanged
// — so even a multi-gigabyte data file is never fully read into memory.
// Leaves the file completely untouched (no write at all) when its
// preamble doesn't need a change.
func patchSQLModeFile(path string) error {
	format := detectCompression(path)

	r, err := openCompressed(path)
	if err != nil {
		return err
	}
	defer r.Close()

	br := bufio.NewReaderSize(r, sqlModePreambleWindow)
	head, _ := br.Peek(sqlModePreambleWindow)

	patchedHead, changed := stripRemovedSQLModeValues(head)
	if !changed {
		return nil
	}

	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}

	writeErr := func() error {
		cw, err := newCompressWriter(out, format)
		if err != nil {
			return err
		}
		if _, err := cw.Write(patchedHead); err != nil {
			cw.Close()
			return err
		}
		if _, err := br.Discard(len(head)); err != nil && err != io.EOF {
			cw.Close()
			return err
		}
		if _, err := io.Copy(cw, br); err != nil {
			cw.Close()
			return err
		}
		return cw.Close()
	}()

	if writeErr != nil {
		out.Close()
		os.Remove(tmp)
		return writeErr
	}

	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, path)
}

// stripRemovedSQLModeValues removes any removedSQLModeValues entry
// (whichever of "V,", ",V", or a bare "V" — covering that value's
// position in the comma-separated sql_mode list) from head, reporting
// whether a change was made.
func stripRemovedSQLModeValues(head []byte) ([]byte, bool) {
	text := string(head)
	changed := false
	for _, v := range removedSQLModeValues {
		switch {
		case strings.Contains(text, v+","):
			text = strings.Replace(text, v+",", "", 1)
			changed = true
		case strings.Contains(text, ","+v):
			text = strings.Replace(text, ","+v, "", 1)
			changed = true
		case strings.Contains(text, v):
			text = strings.Replace(text, v, "", 1)
			changed = true
		}
	}
	if !changed {
		return head, false
	}
	return []byte(text), true
}

// dumpMetadataHasQuoteCharacter reports whether dir's own metadata file
// contains mydumper 1.x's "[config]\nquote-character = ..." key — see
// PatchDumpDir. A missing metadata file (or one without that key) means
// an older mydumper produced this dump.
func dumpMetadataHasQuoteCharacter(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "metadata")) // #nosec G304 -- dir is a dump directory dbtool created or downloaded itself
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "quote-character")
}

// dumpMetadataQuoteCharacterIsDoubleQuote reports whether dir's own
// metadata file declares "quote-character = DOUBLE_QUOTE" — i.e. mydumper
// detected ANSI_QUOTES active on the source and wrote every identifier in
// every file of this dump using double quotes rather than backticks (see
// dumpMetadataHasQuoteCharacter and PatchDumpDir for the write side of
// this). Used by RunRestore to decide whether this dump is at risk of a
// myloader race condition around applying that same setting — see
// myloaderRaceWorkaroundFile.
func dumpMetadataQuoteCharacterIsDoubleQuote(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "metadata")) // #nosec G304 -- dir is a dump directory dbtool created or downloaded itself
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "quote-character") {
			continue
		}
		return strings.Contains(line, "DOUBLE_QUOTE")
	}
	return false
}

// patchSchemaFile reads a single schema file (compressed with gzip, zstd,
// or plain), applies zero-date patches via patchSQL, and writes the result
// back in-place — in whichever compression format it was read from — using
// a temp file so the original is never left in a partial state.
func patchSchemaFile(path string, convertAnsiQuotes bool) error {
	format := detectCompression(path)

	r, err := openCompressed(path)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		return err
	}

	patched := patchSQL(string(data), convertAnsiQuotes)
	if patched == string(data) {
		return nil // nothing to do
	}

	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}

	if writeErr := writeCompressed(out, format, []byte(patched)); writeErr != nil {
		out.Close()
		os.Remove(tmp)
		return writeErr
	}

	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, path)
}

// patchSQL applies zero-date and invalid current_timestamp default fixes to a
// block of SQL text. convertAnsiQuotes additionally converts ANSI-style
// double-quoted identifiers to MySQL backticks — see PatchDumpDir for why
// this must only ever be true for a mydumper < 1.0 dump.
func patchSQL(sql string, convertAnsiQuotes bool) string {
	// Pass 1: NOT NULL DEFAULT '0000-...' → NULL DEFAULT NULL
	result := reNotNullZeroDate.ReplaceAllString(sql, "NULL DEFAULT NULL")
	// Pass 2: DEFAULT '0000-...' (already nullable) → DEFAULT NULL
	result = reZeroDate.ReplaceAllString(result, "DEFAULT NULL")
	// Pass 3: date NOT NULL DEFAULT current_timestamp() → date NULL DEFAULT NULL
	// (current_timestamp() is invalid as a default for the date type)
	result = reDateNotNullCurrentTs.ReplaceAllString(result, "${1} NULL DEFAULT NULL")
	// Pass 4: date DEFAULT current_timestamp() (nullable) → date DEFAULT NULL
	result = reDateCurrentTs.ReplaceAllString(result, "${1} DEFAULT NULL")
	// Pass 5 (mydumper < 1.0 dumps only): identifiers in ANSI-style CREATE
	// TABLE statements use double quotes, which default MySQL sessions
	// parse as strings rather than names.
	if convertAnsiQuotes && reDoubleQuotedCreateTable.MatchString(result) {
		result = convertDoubleQuotedIdentifiers(result)
	}
	return result
}

// convertDoubleQuotedIdentifiers converts ANSI SQL identifiers outside strings
// and comments into MySQL backtick identifiers. Double quotes inside a quoted
// identifier are escaped in ANSI SQL by doubling them; MySQL escapes a backtick
// by doubling it. The caller must first confirm the SQL is an ANSI-quoted
// CREATE TABLE statement.
func convertDoubleQuotedIdentifiers(sql string) string {
	var out strings.Builder
	out.Grow(len(sql))

	for i := 0; i < len(sql); {
		switch {
		case sql[i] == '\'':
			end := scanQuotedSQL(sql, i, '\'', '\'')
			out.WriteString(sql[i:end])
			i = end
		case sql[i] == '`':
			end := scanQuotedSQL(sql, i, '`', '`')
			out.WriteString(sql[i:end])
			i = end
		case sql[i] == '#':
			end := scanLineComment(sql, i)
			out.WriteString(sql[i:end])
			i = end
		case i+1 < len(sql) && sql[i] == '-' && sql[i+1] == '-' && (i+2 == len(sql) || sql[i+2] == ' ' || sql[i+2] == '\t'):
			end := scanLineComment(sql, i)
			out.WriteString(sql[i:end])
			i = end
		case i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*':
			end := scanBlockComment(sql, i)
			out.WriteString(sql[i:end])
			i = end
		case sql[i] == '"':
			out.WriteByte('`')
			i++
			for i < len(sql) {
				if sql[i] == '"' {
					if i+1 < len(sql) && sql[i+1] == '"' {
						out.WriteByte('"')
						i += 2
						continue
					}
					out.WriteByte('`')
					i++
					break
				}
				if sql[i] == '`' {
					out.WriteString("``")
				} else {
					out.WriteByte(sql[i])
				}
				i++
			}
		default:
			out.WriteByte(sql[i])
			i++
		}
	}
	return out.String()
}

func scanQuotedSQL(sql string, start int, quote byte, escapedQuote byte) int {
	i := start + 1
	for i < len(sql) {
		if sql[i] == '\\' && quote == '\'' && i+1 < len(sql) {
			i += 2
			continue
		}
		if sql[i] == quote {
			if i+1 < len(sql) && sql[i+1] == escapedQuote {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return len(sql)
}

func scanLineComment(sql string, start int) int {
	i := strings.IndexByte(sql[start:], '\n')
	if i < 0 {
		return len(sql)
	}
	return start + i + 1
}

func scanBlockComment(sql string, start int) int {
	i := strings.Index(sql[start+2:], "*/")
	if i < 0 {
		return len(sql)
	}
	return start + 2 + i + 2
}
