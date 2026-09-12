package db

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"dbtool/internal/logger"
)

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
// It also converts ANSI-style double-quoted identifiers to MySQL backticks.
// This is needed when restoring with myloader ≤ 0.10, which lacks the
// --init-command flag that would otherwise disable strict SQL mode, and makes
// dumps portable to MySQL servers without ANSI_QUOTES enabled.
func PatchDumpDir(dir string) {
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
		if err := patchSchemaFile(fullPath); err != nil {
			logger.Debug("patchDumpDir: failed to patch %s: %v", fullPath, err)
		}
	}
}

// patchSchemaFile reads a single schema file (compressed or plain), applies
// zero-date patches via patchSQL, and writes the result back in-place using a
// temp file so the original is never left in a partial state.
func patchSchemaFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	compressed := strings.HasSuffix(path, ".gz")

	var reader io.Reader = f
	var gz *gzip.Reader
	if compressed {
		gz, err = gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		reader = gz
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	patched := patchSQL(string(data))
	if patched == string(data) {
		return nil // nothing to do
	}

	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}

	writeErr := func() error {
		if compressed {
			gw := gzip.NewWriter(out)
			if _, err := gw.Write([]byte(patched)); err != nil {
				return err
			}
			return gw.Close()
		}
		_, err := io.WriteString(out, patched)
		return err
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

// patchSQL applies zero-date and invalid current_timestamp default fixes to a
// block of SQL text.
func patchSQL(sql string) string {
	// Pass 1: NOT NULL DEFAULT '0000-...' → NULL DEFAULT NULL
	result := reNotNullZeroDate.ReplaceAllString(sql, "NULL DEFAULT NULL")
	// Pass 2: DEFAULT '0000-...' (already nullable) → DEFAULT NULL
	result = reZeroDate.ReplaceAllString(result, "DEFAULT NULL")
	// Pass 3: date NOT NULL DEFAULT current_timestamp() → date NULL DEFAULT NULL
	// (current_timestamp() is invalid as a default for the date type)
	result = reDateNotNullCurrentTs.ReplaceAllString(result, "${1} NULL DEFAULT NULL")
	// Pass 4: date DEFAULT current_timestamp() (nullable) → date DEFAULT NULL
	result = reDateCurrentTs.ReplaceAllString(result, "${1} DEFAULT NULL")
	// Pass 5: identifiers in ANSI-style CREATE TABLE statements use double
	// quotes, which default MySQL sessions parse as strings rather than names.
	if reDoubleQuotedCreateTable.MatchString(result) {
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
