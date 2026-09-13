package db

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"dbtool/internal/logger"
)

// ValidationIssue reports a table whose data file's INSERT statement is
// missing one or more columns that exist in its schema (excluding true
// GENERATED columns, which mydumper legitimately omits).
//
// This class of bug has a real-world cause worth naming: older mydumper
// versions misread MySQL 8's "DEFAULT_GENERATED" marking (used for columns
// with an automatic DEFAULT/ON UPDATE CURRENT_TIMESTAMP) as if it meant the
// column were a true GENERATED ALWAYS AS (...) column, and drop it from the
// INSERT column list. The column's real value is then never captured at all
// — restoring from such a dump silently replaces it with whatever the
// column's DEFAULT evaluates to at restore time.
type ValidationIssue struct {
	Table   string
	Missing []string
}

// insertColumnPrefixBytes bounds how much of a (potentially huge) data file
// is read to find the first INSERT statement's column list. That list always
// appears within the first few hundred bytes of a table's data file,
// regardless of how many rows follow, so reading more than this would only
// waste time/memory on large dumps without finding anything new.
const insertColumnPrefixBytes = 64 * 1024

var (
	createTableNameRe  = regexp.MustCompile("(?is)CREATE\\s+TABLE\\s+`([^`]+)`\\s*\\(")
	generatedColumnRe  = regexp.MustCompile(`(?i)GENERATED\s+ALWAYS\s+AS`)
	insertColumnListRe = regexp.MustCompile("(?is)INSERT\\s+INTO\\s+`[^`]+`\\s*(?:\\(([^)]*)\\))?\\s*VALUES")
)

// ValidateDump checks every table in dir for columns that its schema
// declares but its data file's INSERT statement doesn't explicitly mention.
// It never returns an error for an individual table it can't parse — that
// table is just skipped — since this is a best-effort safety net, not a
// hard requirement for the dump to be considered complete.
func ValidateDump(dir string) []ValidationIssue {
	entries, err := os.ReadDir(dir)
	if err != nil {
		logger.Debug("ValidateDump: cannot read dir %s: %v", dir, err)
		return nil
	}

	var issues []ValidationIssue
	for _, e := range entries {
		if e.IsDir() || !isTableSchemaFile(e.Name()) {
			continue
		}

		schemaPath := filepath.Join(dir, e.Name())
		schemaSQL, err := readAllCompressed(schemaPath)
		if err != nil {
			logger.Debug("ValidateDump: cannot read schema file %s: %v", schemaPath, err)
			continue
		}

		table, columns, generated, ok := parseSchemaColumns(string(schemaSQL))
		if !ok || len(columns) == 0 {
			continue
		}

		base := schemaFileBase(e.Name())
		dataFile := findFirstDataFile(entries, base)
		if dataFile == "" {
			continue // no data file — e.g. a schema-only dump
		}

		prefix, err := readPrefixCompressed(filepath.Join(dir, dataFile), insertColumnPrefixBytes)
		if err != nil {
			logger.Debug("ValidateDump: cannot read data file %s: %v", dataFile, err)
			continue
		}

		insertColumns, hasExplicitList := parseInsertColumns(string(prefix))
		if !hasExplicitList {
			// Either no INSERT was found in the prefix (empty table — nothing
			// to validate) or it's a positional INSERT (no column list, so
			// every column is necessarily included).
			continue
		}

		inInsert := make(map[string]bool, len(insertColumns))
		for _, c := range insertColumns {
			inInsert[c] = true
		}

		var missing []string
		for _, c := range columns {
			if generated[c] || inInsert[c] {
				continue
			}
			missing = append(missing, c)
		}

		if len(missing) > 0 {
			issues = append(issues, ValidationIssue{Table: table, Missing: missing})
		}
	}

	return issues
}

// ReportValidationIssues prints a hard-to-miss warning for each table with
// missing columns and logs the details. It never fails the dump — by the
// time this runs, mydumper has already finished, and a scheduled job
// shouldn't be treated as failed just because this safety net fired; the
// point is to make the problem visible, not to block backups.
func ReportValidationIssues(issues []ValidationIssue) {
	if len(issues) == 0 {
		return
	}

	fmt.Println()
	fmt.Println("WARNING: possible data loss detected in this dump.")
	fmt.Println("The following table(s) have columns in their schema that are missing from")
	fmt.Println("the INSERT statements in their data file. Restoring from this dump will NOT")
	fmt.Println("preserve the original values for these columns — MySQL will apply each")
	fmt.Println("column's DEFAULT instead. This is a known mydumper issue on older versions")
	fmt.Println("with MySQL 8's DEFAULT/ON UPDATE CURRENT_TIMESTAMP columns — consider")
	fmt.Println("upgrading mydumper/myloader and taking a fresh dump.")
	for _, issue := range issues {
		fmt.Printf("  - %s: %s\n", issue.Table, strings.Join(issue.Missing, ", "))
		logger.Warn("dump validation: table %q is missing column(s) from its INSERT statement: %s",
			issue.Table, strings.Join(issue.Missing, ", "))
	}
	fmt.Println()
}

// parseSchemaColumns extracts the table name, every column name declared in
// a CREATE TABLE statement, and which of those columns are true GENERATED
// columns (GENERATED ALWAYS AS (...) — legitimately absent from INSERTs).
func parseSchemaColumns(schemaSQL string) (table string, columns []string, generated map[string]bool, ok bool) {
	loc := createTableNameRe.FindStringSubmatchIndex(schemaSQL)
	if loc == nil {
		return "", nil, nil, false
	}
	table = schemaSQL[loc[2]:loc[3]]

	openParen := loc[1] - 1 // index of the '(' the regex matched
	closeParen := findMatchingParen(schemaSQL, openParen)
	if closeParen < 0 {
		return "", nil, nil, false
	}
	body := schemaSQL[openParen+1 : closeParen]

	generated = make(map[string]bool)
	for _, def := range splitTopLevelCommas(body) {
		def = strings.TrimSpace(def)
		if def == "" || def[0] != '`' {
			continue // not a column definition (PRIMARY KEY / CONSTRAINT / etc.)
		}
		end := scanQuotedSQL(def, 0, '`', '`')
		if end > len(def) {
			continue
		}
		name := def[1 : end-1]
		columns = append(columns, name)
		if generatedColumnRe.MatchString(def[end:]) {
			generated[name] = true
		}
	}

	return table, columns, generated, len(columns) > 0
}

// parseInsertColumns finds the first INSERT INTO statement in sql and
// returns its explicit column list. hasExplicitList is false when no INSERT
// was found at all (e.g. an empty table) or when the INSERT has no column
// list (positional form, INSERT INTO t VALUES (...), which always includes
// every column).
func parseInsertColumns(sql string) (columns []string, hasExplicitList bool) {
	m := insertColumnListRe.FindStringSubmatch(sql)
	if m == nil {
		return nil, false
	}
	colList := m[1]
	if colList == "" {
		return nil, false
	}
	for _, part := range strings.Split(colList, ",") {
		part = strings.Trim(strings.TrimSpace(part), "`")
		if part != "" {
			columns = append(columns, part)
		}
	}
	return columns, true
}

// findMatchingParen returns the index of the ')' that closes the '(' at
// openIdx in s, skipping over quoted sections so parentheses inside string
// literals or quoted identifiers don't confuse the depth count. Returns -1
// if unbalanced.
func findMatchingParen(s string, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(s); {
		switch s[i] {
		case '\'':
			i = scanQuotedSQL(s, i, '\'', '\'')
			continue
		case '`':
			i = scanQuotedSQL(s, i, '`', '`')
			continue
		case '"':
			i = scanQuotedSQL(s, i, '"', '"')
			continue
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return -1
}

// splitTopLevelCommas splits s on commas that are not inside parentheses or
// quotes, so column definitions like `price` decimal(10,2) or
// `state` enum('a','b') aren't split apart.
func splitTopLevelCommas(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); {
		switch s[i] {
		case '\'':
			i = scanQuotedSQL(s, i, '\'', '\'')
			continue
		case '`':
			i = scanQuotedSQL(s, i, '`', '`')
			continue
		case '"':
			i = scanQuotedSQL(s, i, '"', '"')
			continue
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
		i++
	}
	parts = append(parts, s[start:])
	return parts
}

// schemaFileBase strips the "-schema.sql[.gz]" suffix from a table schema
// filename, leaving the "<db>.<table>" prefix shared with its data file(s).
func schemaFileBase(name string) string {
	idx := strings.Index(name, "-schema.sql")
	if idx < 0 {
		return ""
	}
	return name[:idx]
}

// findFirstDataFile returns the first data file belonging to base among
// entries (sorted, so results are deterministic across runs) — one chunk is
// enough since mydumper's column list is consistent across all of a table's
// chunk files within a single dump.
func findFirstDataFile(entries []os.DirEntry, base string) string {
	if base == "" {
		return ""
	}
	prefix := base + "."

	var candidates []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || strings.Contains(name, "-schema") {
			continue
		}
		if !strings.HasSuffix(name, ".sql") && !strings.HasSuffix(name, ".sql.gz") && !strings.HasSuffix(name, ".sql.zst") {
			continue
		}
		candidates = append(candidates, name)
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.Strings(candidates)
	return candidates[0]
}

// readAllCompressed reads the entire contents of path, transparently
// decompressing (see openCompressed). Schema files are small (a handful of
// KB at most), so reading them fully is fine.
func readAllCompressed(path string) ([]byte, error) {
	r, err := openCompressed(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// readPrefixCompressed reads at most maxBytes from path, transparently
// decompressing (see openCompressed). Used for data files, which can be
// arbitrarily large — only the beginning is needed to find the first
// INSERT statement's column list.
func readPrefixCompressed(path string, maxBytes int64) ([]byte, error) {
	r, err := openCompressed(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, maxBytes))
}
