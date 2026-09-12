package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// systemSchemas are the built-in MySQL schemas that should never appear in user
// schema lists.
var systemSchemas = map[string]bool{
	"information_schema": true,
	"performance_schema": true,
	"mysql":              true,
	"sys":                true,
}

// FetchSchemas returns all non-system schemas visible to the given user.
func FetchSchemas(host, port, user, pass string) ([]string, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/", user, pass, host, port)

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rows, err := conn.Query("SHOW DATABASES")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		if !systemSchemas[strings.ToLower(name)] {
			schemas = append(schemas, name)
		}
	}

	return schemas, rows.Err()
}

// FetchTables Fetch all tables from information_schema (authoritative source)
func FetchTables(host, port, user, pass, dbName string) ([]string, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/information_schema",
		user, pass, host, port,
	)

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	query := `
		SELECT TABLE_NAME
		FROM TABLES
		WHERE TABLE_SCHEMA = ?
	`

	rows, err := conn.Query(query, dbName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string

	for rows.Next() {
		var t string
		rows.Scan(&t)
		tables = append(tables, t)
	}

	return tables, nil
}

// FindLatestDumpDir returns the most recently created directory inside workDir
// whose name starts with prefix followed by an underscore and a timestamp in
// the form YYYY-MM-DD_HHMMSS (the format produced by RunDump).
// Returns an empty string when no matching directory is found.
func FindLatestDumpDir(workDir, prefix string) string {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return ""
	}

	const layout = "2006-01-02_150405"
	var latestDir string
	var latestTime time.Time

	needle := prefix + "_"

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		name := e.Name()
		if !strings.HasPrefix(name, needle) {
			continue
		}

		tsPart := strings.TrimPrefix(name, needle)
		t, err := time.ParseInLocation(layout, tsPart, time.Local)
		if err != nil {
			continue
		}

		if latestDir == "" || t.After(latestTime) {
			latestDir = filepath.Join(workDir, name)
			latestTime = t
		}
	}

	return latestDir
}
