package db

import (
	"database/sql"
	"fmt"
	"strings"

	"dbtool/internal/types"

	_ "github.com/go-sql-driver/mysql"
)

// firstReadOnlyVariableOn returns the first of "read_only"/"super_read_only"
// (checked in that order — super_read_only always implies read_only, so
// reporting read_only first is the more informative answer) whose value in
// values is "ON", or "" if neither is.
func firstReadOnlyVariableOn(values map[string]string) string {
	for _, name := range []string{"read_only", "super_read_only"} {
		if strings.EqualFold(values[name], "ON") {
			return name
		}
	}
	return ""
}

// serverIdentity is what checkDestinationServer fetches from a single
// connection: which physical server actually answered, and whether it's
// currently rejecting writes. restoreOneDatabase logs this before each
// backoff-delayed retry, so a restore failure can be cross-checked against
// what server dbtool was actually talking to. A bare read_only=OFF reading
// (checked from a single, separate connection) can't rule out dbtool's
// control connection and myloader's own connections landing on different
// physical servers behind the same configured host/port — e.g. a stale DNS
// entry, a connection pooler, or an SSH tunnel pointed at the wrong
// internal address — @@hostname/@@server_id/@@port can.
type serverIdentity struct {
	hostname    string
	serverID    string
	port        string
	readOnlyVar string // "" if neither read_only nor super_read_only is ON
}

// checkDestinationServer opens a fresh connection to cfg's server (the same
// host:port myloader itself will be told to use) and fetches its identity
// and read_only/super_read_only status in that one connection.
func checkDestinationServer(cfg types.Config, pass string) (serverIdentity, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/", cfg.User, pass, cfg.Host, cfg.Port)
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return serverIdentity{}, err
	}
	defer conn.Close()

	var id serverIdentity
	if err := conn.QueryRow("SELECT @@hostname, @@server_id, @@port").Scan(&id.hostname, &id.serverID, &id.port); err != nil {
		return serverIdentity{}, fmt.Errorf("fetching server identity via %s:%s: %w", cfg.Host, cfg.Port, err)
	}

	// SHOW isn't part of MySQL's preparable-statement grammar, so a "?"
	// placeholder here comes back as a literal syntax error ("near '?'")
	// instead of being substituted — the two names below are fixed
	// constants this function controls, never external input, so building
	// the literal directly is safe.
	values := make(map[string]string, 2)
	for _, name := range []string{"read_only", "super_read_only"} {
		var varName, value string
		query := fmt.Sprintf("SHOW VARIABLES LIKE '%s'", name)
		if err := conn.QueryRow(query).Scan(&varName, &value); err != nil {
			return serverIdentity{}, fmt.Errorf("checking %s via %s:%s: %w", name, cfg.Host, cfg.Port, err)
		}
		values[varName] = value
	}
	id.readOnlyVar = firstReadOnlyVariableOn(values)
	return id, nil
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
