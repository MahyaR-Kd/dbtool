package rotate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dbtool/internal/logger"
	"dbtool/internal/s3store"
	"dbtool/internal/settings"
)

// dumpTimeLayout matches the timestamp suffix used in dump directory names
// produced by db.RunDump: "<configName>_YYYY-MM-DD_HHMMSS".
const dumpTimeLayout = "2006-01-02_150405"

// RotateOldDumps removes dump directories inside workDir that belong to
// configName and are older than retentionDays days.
// When retentionDays is 0 (or negative) the function does nothing, preserving
// all dumps permanently.
func RotateOldDumps(workDir, configName string, retentionDays int) {
	if retentionDays <= 0 {
		logger.Debug("rotate: retention not set, skipping rotation for config %q", configName)
		return
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	prefix := configName + "_"

	entries, err := os.ReadDir(workDir)
	if err != nil {
		logger.Error("rotate: failed to read work directory %q: %v", workDir, err)
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		// Extract the timestamp portion after "<configName>_".
		tsPart := strings.TrimPrefix(name, prefix)
		t, err := time.ParseInLocation(dumpTimeLayout, tsPart, time.Local)
		if err != nil {
			// Not a dump directory we created; leave it alone.
			logger.Debug("rotate: skipping %q (cannot parse timestamp: %v)", name, err)
			continue
		}

		if t.Before(cutoff) {
			fullPath := filepath.Join(workDir, name)
			if err := os.RemoveAll(fullPath); err != nil {
				logger.Error("rotate: failed to remove old dump %q: %v", fullPath, err)
				fmt.Printf("Warning: could not remove old dump %q: %v\n", fullPath, err)
			} else {
				logger.Info("rotate: removed old dump %q (older than %d days)", fullPath, retentionDays)
				fmt.Printf("Rotated (removed) old dump: %s\n", fullPath)
			}
		}
	}
}

// RotateOldS3Dumps deletes dump directories in S3 that belong to configName
// and are older than retentionDays days.
// When retentionDays is 0 (or negative) the function does nothing.
func RotateOldS3Dumps(cfg settings.S3Config, configName string, retentionDays int) {
	if retentionDays <= 0 {
		logger.Debug("rotate: S3 retention not set, skipping S3 rotation for config %q", configName)
		return
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	prefix := configName + "_"

	dumps, err := s3store.ListDumps(cfg)
	if err != nil {
		logger.Error("rotate: failed to list S3 dumps: %v", err)
		return
	}

	for _, name := range dumps {
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		tsPart := strings.TrimPrefix(name, prefix)
		t, err := time.ParseInLocation(dumpTimeLayout, tsPart, time.Local)
		if err != nil {
			logger.Debug("rotate: skipping S3 dump %q (cannot parse timestamp: %v)", name, err)
			continue
		}

		if t.Before(cutoff) {
			if err := s3store.DeleteDump(cfg, name); err != nil {
				logger.Error("rotate: failed to delete S3 dump %q: %v", name, err)
				fmt.Printf("Warning: could not remove old S3 dump %q: %v\n", name, err)
			} else {
				logger.Info("rotate: removed old S3 dump %q (older than %d days)", name, retentionDays)
				fmt.Printf("Rotated (removed) old S3 dump: %s\n", name)
			}
		}
	}
}
