package job

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dbtool/internal/logger"
	"dbtool/internal/paths"
	"dbtool/internal/secret"
)

func jobFilePath() string {
	dir, err := paths.DbtoolDir()
	if err != nil {
		return filepath.Join(os.ExpandEnv("$HOME"), ".dbtool", "dbtool.jobs")
	}
	return filepath.Join(dir, "dbtool.jobs")
}

// Load reads all saved job configs from disk, decrypting SrcPassword and
// DstPassword so callers get plaintext ready to use.
// Format: name|type|schedule|srcConfigName|srcPassword|dstConfigName|dstPassword|overwriteTables
//
// Jobs found with legacy plaintext passwords (from installs predating
// encryption-at-rest) are transparently re-saved encrypted.
func Load() []Config {
	jobs, needsMigration := load()

	if needsMigration {
		logger.Info("jobs: migrating plaintext credentials to encrypted storage")
		Overwrite(jobs)
	}

	return jobs
}

func load() (jobs []Config, needsMigration bool) {
	file, err := os.Open(jobFilePath())
	if err != nil {
		return []Config{}, false
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			fmt.Println("Failed to close job file:", err)
			logger.Error("Failed to close job file: %v", err)
		}
	}(file)

	decrypt := func(field, value string) string {
		pt, wasEncrypted, err := secret.DecryptField(value)
		if err != nil {
			logger.Error("jobs: failed to decrypt %s, treating as unset: %v", field, err)
			return ""
		}
		if !wasEncrypted {
			needsMigration = true
		}
		return pt
	}

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.Split(line, "|")

		if len(parts) < 7 {
			continue
		}

		j := Config{
			Name:          parts[0],
			Type:          parts[1],
			Schedule:      parts[2],
			SrcConfigName: parts[3],
			SrcPassword:   decrypt("src_password", parts[4]),
			DstConfigName: parts[5],
			DstPassword:   decrypt("dst_password", parts[6]),
		}
		if len(parts) >= 8 {
			j.OverwriteTables = parts[7] == "true"
		}

		jobs = append(jobs, j)
	}

	return jobs, needsMigration
}

// Save appends a new job config to the jobs file, encrypting SrcPassword and
// DstPassword before they touch disk.
func Save(j Config) {
	path := jobFilePath()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Println("Failed to save job:", err)
		return
	}
	// OpenFile's mode argument only applies when creating a new file; chmod
	// explicitly so a pre-existing file with looser permissions (e.g. from a
	// version predating credential storage) is tightened too.
	if err := os.Chmod(path, 0600); err != nil {
		logger.Error("Failed to set permissions on job file: %v", err)
	}
	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
			fmt.Println("Failed to close job file:", err)
			logger.Error("Failed to close job file: %v", err)
		}
	}(f)

	line, err := encodeLine(j)
	if err != nil {
		fmt.Println("Failed to encrypt job credentials:", err)
		logger.Error("Failed to encrypt job credentials for %q: %v", j.Name, err)
		return
	}
	if _, err := f.WriteString(line); err != nil {
		fmt.Println("Failed to write job:", err)
	}
}

// Overwrite replaces the entire jobs file with the given list, encrypting
// SrcPassword and DstPassword before they touch disk.
func Overwrite(jobs []Config) {
	path := jobFilePath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		fmt.Println("Failed to overwrite jobs file:", err)
		return
	}
	if err := os.Chmod(path, 0600); err != nil {
		logger.Error("Failed to set permissions on job file: %v", err)
	}
	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
			fmt.Println("Failed to close job file:", err)
			logger.Error("Failed to close job file: %v", err)
		}
	}(f)

	for _, j := range jobs {
		line, err := encodeLine(j)
		if err != nil {
			fmt.Println("Failed to encrypt job credentials:", err)
			logger.Error("Failed to encrypt job credentials for %q: %v", j.Name, err)
			continue
		}
		if _, err := f.WriteString(line); err != nil {
			fmt.Println("Failed to write job:", err)
		}
	}
}

// encodeLine renders j as one pipe-delimited storage line with its
// credential fields encrypted.
func encodeLine(j Config) (string, error) {
	srcPassword, err := secret.EncryptField(j.SrcPassword)
	if err != nil {
		return "", fmt.Errorf("encrypt src password: %w", err)
	}
	dstPassword, err := secret.EncryptField(j.DstPassword)
	if err != nil {
		return "", fmt.Errorf("encrypt dst password: %w", err)
	}

	return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%v\n",
		j.Name, j.Type, j.Schedule,
		j.SrcConfigName, srcPassword,
		j.DstConfigName, dstPassword,
		j.OverwriteTables), nil
}
