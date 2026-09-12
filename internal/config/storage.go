package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"dbtool/internal/credvault"
	"dbtool/internal/logger"
	"dbtool/internal/paths"
	"dbtool/internal/types"
)

func configFilePath() string {
	dir, err := paths.DbtoolDir()
	if err != nil {
		return filepath.Join(os.ExpandEnv("$HOME"), ".dbtool", "dbtool.conf")
	}
	return filepath.Join(dir, "dbtool.conf")
}

// Load reads all saved configs from disk. A config's Password field, if
// set, is left in its still-encrypted form — decrypting it requires the
// master password, and Load must not prompt for that just to list or look
// up configs. Callers that actually need the plaintext (dump/restore)
// decrypt on demand via AskPassword.
func Load() []types.Config {
	file, err := os.Open(configFilePath())
	if err != nil {
		return []types.Config{}
	}
	defer file.Close()

	var configs []types.Config
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.Split(line, "|")

		if len(parts) < 4 {
			continue
		}

		c := types.Config{
			Name: parts[0],
			Host: parts[1],
			Port: parts[2],
			User: parts[3],
		}

		if len(parts) >= 9 {
			c.SSH = parts[4] == "true"
			c.SSHHost = parts[5]
			c.SSHUser = parts[6]
			c.SSHPort = parts[7]
			c.LocalPort = parts[8]
		}

		// field 10 (index 9): comma-separated ignored schemas
		if len(parts) >= 10 && parts[9] != "" {
			c.IgnoredSchemas = strings.Split(parts[9], ",")
		}
		// field 11 (index 10): dump retention days (0 means keep permanently)
		if len(parts) >= 11 && parts[10] != "" {
			if days, err := strconv.Atoi(parts[10]); err == nil && days >= 0 {
				c.RetentionDays = days
			}
		}
		// field 12 (index 11): per-schema ignored tables encoded as
		// "schema1:tbl1,tbl2;schema2:tbl3"
		if len(parts) >= 12 && parts[11] != "" {
			c.IgnoredTables = parseIgnoredTables(parts[11])
		}
		// field 13 (index 12): no-locks flag
		if len(parts) >= 13 && parts[12] == "true" {
			c.NoLocks = true
		}
		// field 14 (index 13): master-password-encrypted DB password, if saved
		if len(parts) >= 14 {
			c.Password = parts[13]
		}

		configs = append(configs, c)
	}

	return configs
}

// Save appends a new config to the config file, encrypting its Password
// field (if set) under the master password before it touches disk.
func Save(c types.Config) {
	line, err := encodeLine(c)
	if err != nil {
		fmt.Println("Failed to encrypt config password:", err)
		logger.Error("failed to encrypt config password for %q: %v", c.Name, err)
		return
	}

	path := configFilePath()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Println("Failed to save config:", err)
		return
	}
	defer f.Close()
	if err := os.Chmod(path, 0600); err != nil {
		logger.Error("failed to set permissions on config file: %v", err)
	}

	f.WriteString(line)
}

// Overwrite replaces the entire config file with the given list, encrypting
// each config's Password field (if set) under the master password before
// it touches disk.
func Overwrite(configs []types.Config) {
	path := configFilePath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		fmt.Println("Failed to overwrite config file")
		return
	}
	defer f.Close()
	if err := os.Chmod(path, 0600); err != nil {
		logger.Error("failed to set permissions on config file: %v", err)
	}

	for _, c := range configs {
		line, err := encodeLine(c)
		if err != nil {
			fmt.Println("Failed to encrypt config password:", err)
			logger.Error("failed to encrypt config password for %q: %v", c.Name, err)
			continue
		}
		f.WriteString(line)
	}
}

// ResetMasterPassword discards the master password's verification record
// (see credvault.Reset) and clears every saved config's stored Password
// field, since those would otherwise be permanently undecryptable
// ciphertext referencing a master password that no longer verifies against
// anything.
func ResetMasterPassword() error {
	if err := credvault.Reset(); err != nil {
		return err
	}

	configs := Load()
	changed := false
	for i := range configs {
		if configs[i].Password != "" {
			configs[i].Password = ""
			changed = true
		}
	}
	if changed {
		Overwrite(configs)
	}
	return nil
}

// ChangeMasterPassword re-encrypts every saved config password under a
// newly-chosen master password, without clearing them — unlike
// ResetMasterPassword, existing saved passwords survive. Prompts for the
// current master password once (to verify and decrypt existing values) and
// a new one once (create + confirm). Configs with no saved password are
// left untouched. If it returns an error, dbtool.conf is not modified —
// re-encryption happens entirely in memory before anything is written.
func ChangeMasterPassword() error {
	configs := Load()

	var encrypted []string
	var idxs []int
	for i, c := range configs {
		if c.Password != "" {
			encrypted = append(encrypted, c.Password)
			idxs = append(idxs, i)
		}
	}

	reencrypted, err := credvault.Rotate(encrypted)
	if err != nil {
		return err
	}

	if len(idxs) == 0 {
		return nil
	}
	for j, i := range idxs {
		configs[i].Password = reencrypted[j]
	}
	Overwrite(configs)
	return nil
}

// encodeLine renders c as one pipe-delimited storage line, encrypting its
// Password field under the master password. EncryptField is a no-op for an
// already-encrypted value (carried over unmodified from Load) or an empty
// one, so this is safe to call whether c.Password holds fresh plaintext the
// user just typed or ciphertext from a previous load.
func encodeLine(c types.Config) (string, error) {
	encPassword, err := credvault.EncryptField(c.Password)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s|%s|%s|%s|%t|%s|%s|%s|%s|%s|%d|%s|%t|%s\n",
		c.Name, c.Host, c.Port, c.User,
		c.SSH, c.SSHHost, c.SSHUser, c.SSHPort, c.LocalPort,
		strings.Join(c.IgnoredSchemas, ","), c.RetentionDays,
		serializeIgnoredTables(c.IgnoredTables),
		c.NoLocks,
		encPassword), nil
}

// serializeIgnoredTables encodes a map[schema][]table as
// "schema1:tbl1,tbl2;schema2:tbl3". Returns "" for nil/empty maps.
func serializeIgnoredTables(m map[string][]string) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	for schema, tables := range m {
		if len(tables) > 0 {
			parts = append(parts, schema+":"+strings.Join(tables, ","))
		}
	}
	return strings.Join(parts, ";")
}

// parseIgnoredTables decodes the format produced by serializeIgnoredTables.
func parseIgnoredTables(s string) map[string][]string {
	result := make(map[string][]string)
	for _, entry := range strings.Split(s, ";") {
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
	return result
}
