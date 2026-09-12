package config

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"

	"dbtool/internal/connection"
	"dbtool/internal/credvault"
	"dbtool/internal/interactivelist"
	"dbtool/internal/secureinput"
	"dbtool/internal/settings"
	"dbtool/internal/types"
)

func AskInteractive() types.Config {
	reader := bufio.NewReader(os.Stdin)
	var cfg types.Config

	fmt.Print("Name: ")
	cfg.Name, _ = reader.ReadString('\n')
	cfg.Name = strings.TrimSpace(cfg.Name)

	fmt.Print("Host: ")
	cfg.Host, _ = reader.ReadString('\n')
	cfg.Host = strings.TrimSpace(cfg.Host)

	fmt.Print("Port: ")
	cfg.Port, _ = reader.ReadString('\n')
	cfg.Port = strings.TrimSpace(cfg.Port)

	fmt.Print("User: ")
	cfg.User, _ = reader.ReadString('\n')
	cfg.User = strings.TrimSpace(cfg.User)

	cfg.RetentionDays = askRetentionDays(reader, 0)

	fmt.Print("Use SSH? (true/false): ")
	sshStr, _ := reader.ReadString('\n')
	sshStr = strings.TrimSpace(sshStr)
	cfg.SSH = strings.EqualFold(sshStr, "true")

	if cfg.SSH {
		fmt.Print("SSH Host: ")
		cfg.SSHHost, _ = reader.ReadString('\n')
		cfg.SSHHost = strings.TrimSpace(cfg.SSHHost)

		fmt.Print("SSH User: ")
		cfg.SSHUser, _ = reader.ReadString('\n')
		cfg.SSHUser = strings.TrimSpace(cfg.SSHUser)

		fmt.Print("SSH Port: ")
		cfg.SSHPort, _ = reader.ReadString('\n')
		cfg.SSHPort = strings.TrimSpace(cfg.SSHPort)
	}

	fmt.Print("Skip global lock (--no-locks)? Needed if user lacks RELOAD privilege (true/false) [false]: ")
	noLocksStr, _ := reader.ReadString('\n')
	noLocksStr = strings.TrimSpace(noLocksStr)
	cfg.NoLocks = strings.EqualFold(noLocksStr, "true")

	pass, err := secureinput.ReadPassword("DB Password (used to fetch schema list): ")
	if err != nil {
		fmt.Println("Error reading password:", err)
		pass = ""
	}
	cfg.Password = maybeSavePassword(reader, pass)

	s := settings.Load()

	originalHost, originalPort := cfg.Host, cfg.Port
	cfg, cleanup := connection.ApplyTunnels(cfg, s)
	defer cleanup()

	cfg.IgnoredSchemas, cfg.IgnoredTables, _ = askIgnoredSchemasAndTables(reader, cfg.Host, cfg.Port, cfg.User, pass, nil, nil)

	// Restore the original host/port so the saved config points at the real
	// database address, not the ephemeral tunnel endpoint.
	cfg.Host, cfg.Port = originalHost, originalPort

	return cfg
}

// EditInteractive prompts the user to update each field of an existing Config.
// Pressing Enter on any field keeps its current value.
func EditInteractive(cfg types.Config) types.Config {
	reader := bufio.NewReader(os.Stdin)

	readField := func(prompt, current string) string {
		fmt.Printf("%s [%s]: ", prompt, current)
		val, _ := reader.ReadString('\n')
		val = strings.TrimSpace(val)
		if val == "" {
			return current
		}
		return val
	}

	cfg.Name = readField("Name", cfg.Name)
	cfg.Host = readField("Host", cfg.Host)
	cfg.Port = readField("Port", cfg.Port)
	cfg.User = readField("User", cfg.User)
	cfg.RetentionDays = askRetentionDays(reader, cfg.RetentionDays)

	fmt.Printf("Use SSH? (true/false) [%t]: ", cfg.SSH)
	sshStr, _ := reader.ReadString('\n')
	sshStr = strings.TrimSpace(sshStr)
	if sshStr != "" {
		cfg.SSH = strings.EqualFold(sshStr, "true")
	}

	if cfg.SSH {
		cfg.SSHHost = readField("SSH Host", cfg.SSHHost)
		cfg.SSHUser = readField("SSH User", cfg.SSHUser)
		cfg.SSHPort = readField("SSH Port", cfg.SSHPort)
	}

	fmt.Printf("Skip global lock (--no-locks)? [%t]: ", cfg.NoLocks)
	noLocksStr, _ := reader.ReadString('\n')
	noLocksStr = strings.TrimSpace(noLocksStr)
	if noLocksStr != "" {
		cfg.NoLocks = strings.EqualFold(noLocksStr, "true")
	}

	savedLabel := "no"
	if cfg.Password != "" {
		savedLabel = "yes"
	}
	fmt.Printf("Update saved DB password? (currently saved: %s) (y/n) [n]: ", savedLabel)
	updatePassAns, _ := reader.ReadString('\n')
	updatePassAns = strings.TrimSpace(strings.ToLower(updatePassAns))
	if updatePassAns == "y" || updatePassAns == "yes" {
		newPass, err := secureinput.ReadPassword("New DB password (press Enter to clear the saved password): ")
		if err != nil {
			fmt.Println("Error reading password:", err)
		} else {
			cfg.Password = newPass
		}
	}

	fmt.Print("Update ignored schemas/tables? (y/n) [n]: ")
	ans, _ := reader.ReadString('\n')
	ans = strings.TrimSpace(strings.ToLower(ans))
	if ans == "y" || ans == "yes" {
		pass, err := secureinput.ReadPassword("DB Password (used only to fetch schema list, not stored): ")
		if err != nil {
			fmt.Println("Error reading password:", err)
			pass = ""
		}
		s := settings.Load()

		existingSchemas := cfg.IgnoredSchemas
		existingTables := cfg.IgnoredTables
		tunnelCfg, cleanup := connection.ApplyTunnels(cfg, s)
		defer cleanup()

		cfg.IgnoredSchemas, cfg.IgnoredTables, _ = askIgnoredSchemasAndTables(reader, tunnelCfg.Host, tunnelCfg.Port, tunnelCfg.User, pass, existingSchemas, existingTables)
	}

	return cfg
}

// maybeSavePassword asks whether to save pass (encrypted under the master
// password) for future dump/restore use. Returns pass if the user agrees,
// or "" otherwise (including when pass is itself empty — nothing to save).
func maybeSavePassword(reader *bufio.Reader, pass string) string {
	if pass == "" {
		return ""
	}
	fmt.Print("Save this password for future dump/restore (encrypted with your master password)? (y/n) [n]: ")
	ans, _ := reader.ReadString('\n')
	ans = strings.TrimSpace(strings.ToLower(ans))
	if ans == "y" || ans == "yes" {
		return pass
	}
	return ""
}

// parseIgnoredTablesInput parses a user-provided string of the form
// "schema1:tbl1,tbl2;schema2:tbl3" into a map.
func parseIgnoredTablesInput(input string) map[string][]string {
	result := make(map[string][]string)
	for _, entry := range strings.Split(input, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		idx := strings.Index(entry, ":")
		if idx < 0 {
			continue
		}
		schema := strings.TrimSpace(entry[:idx])
		tablesRaw := strings.TrimSpace(entry[idx+1:])
		if schema == "" || tablesRaw == "" {
			continue
		}
		var tables []string
		for _, t := range strings.Split(tablesRaw, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tables = append(tables, t)
			}
		}
		if len(tables) > 0 {
			result[schema] = tables
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// parseSchemaList splits a comma-separated string of schema names into a slice,
// trimming whitespace and dropping empty entries.
func parseSchemaList(input string) []string {
	var schemas []string
	for _, s := range strings.Split(input, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			schemas = append(schemas, s)
		}
	}
	return schemas
}

// askIgnoredSchemasAndTables connects to the DB, lets the user choose which
// schemas get dumped — either by picking ones to IGNORE (exclude a few out
// of many) or by picking ones to INCLUDE (select a few out of many, useful
// when most schemas should be skipped) — then offers to restrict specific
// tables within the remaining (non-ignored) schemas the same way. Returns
// both slices in the same IgnoredSchemas/IgnoredTables shape dbtool has
// always stored, regardless of which mode was used to choose them.
// existingSchemas and existingTables carry the previously saved values so
// that an empty selection can ask the user whether to clear them.
func askIgnoredSchemasAndTables(reader *bufio.Reader, host, port, user, pass string, existingSchemas []string, existingTables map[string][]string) ([]string, map[string][]string, error) {
	// Route through SOCKS5 proxy if configured.
	connectHost, connectPort := host, port

	schemas, err := fetchUserSchemas(connectHost, connectPort, user, pass)
	if err != nil {
		fmt.Printf("Could not fetch schemas (%v) — skipping schema/table selection.\n", err)
		return nil, nil, err
	}

	if len(schemas) == 0 {
		fmt.Println("No user schemas found.")
		return nil, nil, nil
	}

	existingIgnoredSet := make(map[string]bool, len(existingSchemas))
	for _, s := range existingSchemas {
		existingIgnoredSet[s] = true
	}

	// --- Step 1: choose which schemas get dumped ---
	mode, err := interactivelist.SelectOne("How do you want to choose which schemas to dump?", []string{
		"Ignore selected schemas (dump everything except these)",
		"Include only selected schemas (handy when you only want a few out of many)",
	})
	if err != nil {
		fmt.Println("Selection canceled — keeping existing schema/table settings unchanged.")
		return existingSchemas, existingTables, nil
	}
	includeMode := mode == 1

	ignoredSchemaSet := map[string]bool{}
	var ignoredSchemas []string

	if includeMode {
		// Pre-check whatever wasn't previously ignored, i.e. what's currently included —
		// so leaving everything as-is and pressing Enter reproduces the current config.
		preselect := func(i int) bool { return !existingIgnoredSet[schemas[i]] }
		selectedIdx, err := interactivelist.SelectMulti("Include schemas (Tab to toggle, Enter to confirm)", schemas, preselect)
		if err != nil {
			fmt.Println("Selection canceled — keeping existing schema/table settings unchanged.")
			return existingSchemas, existingTables, nil
		}

		if len(selectedIdx) == 0 {
			fmt.Print("No schemas selected — that would ignore ALL schemas and dump nothing. Include all schemas instead? (Y/n): ")
			confirm, _ := reader.ReadString('\n')
			confirm = strings.TrimSpace(strings.ToLower(confirm))
			if confirm == "" || confirm == "y" || confirm == "yes" {
				for i := range schemas {
					selectedIdx = append(selectedIdx, i)
				}
			}
		}

		includedSet := make(map[string]bool, len(selectedIdx))
		for _, i := range selectedIdx {
			includedSet[schemas[i]] = true
		}
		for _, s := range schemas {
			if !includedSet[s] {
				ignoredSchemas = append(ignoredSchemas, s)
				ignoredSchemaSet[s] = true
			}
		}
	} else {
		preselect := func(i int) bool { return existingIgnoredSet[schemas[i]] }
		selectedIdx, err := interactivelist.SelectMulti("Ignore schemas (Tab to toggle, Enter to confirm)", schemas, preselect)
		if err != nil {
			fmt.Println("Selection canceled — keeping existing schema/table settings unchanged.")
			return existingSchemas, existingTables, nil
		}
		for _, i := range selectedIdx {
			ignoredSchemas = append(ignoredSchemas, schemas[i])
			ignoredSchemaSet[schemas[i]] = true
		}
	}

	// --- Step 2: pick tables to ignore within non-ignored schemas ---
	var dumpableSchemas []string
	for _, s := range schemas {
		if !ignoredSchemaSet[s] {
			dumpableSchemas = append(dumpableSchemas, s)
		}
	}

	ignoredTables, err := askIgnoredTablesForSchemas(reader, dumpableSchemas, connectHost, connectPort, user, pass, existingTables)
	if err != nil {
		return nil, nil, err
	}
	return ignoredSchemas, ignoredTables, nil
}

// askIgnoredTablesForSchemas loops asking the user to pick schemas and then
// tables within those schemas to ignore. dumpableSchemas is the list of schemas
// that are NOT fully ignored (and thus will be dumped).
// existingTables carries the previously saved per-schema ignored tables so that
// an empty answer to the opening question can ask the user whether to clear them.
func askIgnoredTablesForSchemas(reader *bufio.Reader, dumpableSchemas []string, host, port, user, pass string, existingTables map[string][]string) (map[string][]string, error) {
	if len(dumpableSchemas) == 0 {
		return nil, nil
	}

	fmt.Print("Do you want to restrict tables in any schema? (y/n) [n]: ")
	ans, _ := reader.ReadString('\n')
	ans = strings.TrimSpace(strings.ToLower(ans))
	if ans != "y" && ans != "yes" {
		return existingTables, nil
	}

	mode, err := interactivelist.SelectOne("How do you want to choose tables within a schema?", []string{
		"Ignore selected tables (dump everything else in that schema)",
		"Include only selected tables (handy when you only want a few out of many)",
	})
	if err != nil {
		fmt.Println("Selection canceled — keeping existing table settings unchanged.")
		return existingTables, nil
	}
	includeMode := mode == 1

	// Start from the existing map so schemas the user doesn't touch this
	// session keep whatever was configured for them before.
	ignoredTables := make(map[string][]string, len(existingTables))
	for schema, tables := range existingTables {
		ignoredTables[schema] = tables
	}

	for {
		schemaIdx, err := interactivelist.SelectOne("Choose a schema to configure (or cancel to finish)", dumpableSchemas)
		if err != nil {
			break
		}
		chosenSchema := dumpableSchemas[schemaIdx]

		tables, err := fetchTablesForSchema(host, port, user, pass, chosenSchema)
		switch {
		case err != nil:
			fmt.Printf("Could not fetch tables for %q (%v) — skipping.\n", chosenSchema, err)
		case len(tables) == 0:
			fmt.Printf("No tables found in schema %q.\n", chosenSchema)
		default:
			existingIgnoredSet := make(map[string]bool, len(existingTables[chosenSchema]))
			for _, t := range existingTables[chosenSchema] {
				existingIgnoredSet[t] = true
			}

			var selectedIdx []int
			var pickErr error
			if includeMode {
				preselect := func(i int) bool { return !existingIgnoredSet[tables[i]] }
				selectedIdx, pickErr = interactivelist.SelectMulti(
					fmt.Sprintf("Include tables in %q (Tab to toggle, Enter to confirm)", chosenSchema), tables, preselect)
			} else {
				preselect := func(i int) bool { return existingIgnoredSet[tables[i]] }
				selectedIdx, pickErr = interactivelist.SelectMulti(
					fmt.Sprintf("Ignore tables in %q (Tab to toggle, Enter to confirm)", chosenSchema), tables, preselect)
			}

			if pickErr != nil {
				fmt.Println("Selection canceled for this schema — leaving it unchanged.")
			} else {
				var newIgnored []string
				if includeMode {
					if len(selectedIdx) == 0 {
						fmt.Printf("No tables selected — including all tables in %q (nothing ignored there).\n", chosenSchema)
					} else {
						includedSet := make(map[string]bool, len(selectedIdx))
						for _, i := range selectedIdx {
							includedSet[tables[i]] = true
						}
						for _, t := range tables {
							if !includedSet[t] {
								newIgnored = append(newIgnored, t)
							}
						}
					}
				} else {
					for _, i := range selectedIdx {
						newIgnored = append(newIgnored, tables[i])
					}
				}

				if len(newIgnored) > 0 {
					ignoredTables[chosenSchema] = newIgnored
				} else {
					// Empty result for this schema (all included) — remove it from the map.
					delete(ignoredTables, chosenSchema)
				}
			}
		}

		fmt.Print("Configure tables for another schema? (y/n) [n]: ")
		more, _ := reader.ReadString('\n')
		more = strings.TrimSpace(strings.ToLower(more))
		if more != "y" && more != "yes" {
			break
		}
	}

	if len(ignoredTables) == 0 {
		return nil, nil
	}
	return ignoredTables, nil
}

// fetchUserSchemas connects directly via the Go MySQL driver and returns all
// non-system schema names.
func fetchUserSchemas(host, port, user, pass string) ([]string, error) {
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

	// systemSchemas is defined here (rather than shared with internal/db) to avoid
	// an import cycle: internal/db imports internal/config, so internal/config
	// cannot import internal/db.
	systemSchemas := map[string]bool{
		"information_schema": true,
		"performance_schema": true,
	}

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

// fetchTablesForSchema returns all table names in the given schema.
func fetchTablesForSchema(host, port, user, pass, schema string) ([]string, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/", user, pass, host, port)

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rows, err := conn.Query(fmt.Sprintf("SHOW TABLES IN `%s`", schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

func Select() types.Config {
	configs := Load()

	options := make([]string, len(configs))
	for i, c := range configs {
		options[i] = fmt.Sprintf("%s (%s:%s)", c.Name, c.Host, c.Port)
	}

	idx, err := interactivelist.SelectOne("Select config", options)
	if err != nil {
		fmt.Println("Invalid selection")
		os.Exit(1)
	}

	return configs[idx]
}

// SelectInteractive is an alias for Select, kept for callers that named
// their non-interactive/interactive selection paths explicitly.
func SelectInteractive() types.Config {
	return Select()
}

// SelectByName allows non-interactive selection via config name
func SelectByName(name string) types.Config {
	configs := Load()

	for _, c := range configs {
		if c.Name == name {
			return c
		}
	}

	fmt.Println("Config not found:", name)
	os.Exit(1)
	return types.Config{}
}

// AskPassword resolves a DB password: cliPass (from a --pass flag) if set,
// otherwise the named environment variable if set, otherwise an interactive
// masked prompt. envVar may be "" to skip the environment variable check.
// AskPassword resolves a DB password for cfg, trying in order: cliPass
// (from a --pass flag), the named environment variable, cfg's saved
// password (if one was stored — decrypted with the master password,
// prompting for it at most once per process run), and finally an
// interactive masked prompt. envVar may be "" to skip the environment
// variable check.
func AskPassword(cliPass, envVar string, cfg types.Config) string {
	if cliPass != "" {
		return cliPass
	}
	if envVar != "" {
		if v := os.Getenv(envVar); v != "" {
			return v
		}
	}
	if cfg.Password != "" {
		pt, err := credvault.Decrypt(cfg.Password)
		if err != nil {
			fmt.Println("Could not unlock the saved password:", err)
		} else if pt != "" {
			return pt
		}
	}

	pass, err := secureinput.ReadPassword("DB Password: ")
	if err != nil {
		fmt.Println("Error reading password:", err)
		os.Exit(1)
	}
	return pass
}

func askRetentionDays(reader *bufio.Reader, current int) int {
	if current < 0 {
		current = 0
	}
	for {
		fmt.Printf("Retention days for dumps (0 = keep forever) [%d]: ", current)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			return current
		}
		days, err := strconv.Atoi(input)
		if err != nil || days < 0 {
			fmt.Println("Please enter a non-negative integer.")
			continue
		}
		return days
	}
}
