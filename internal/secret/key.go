// Package secret encrypts credential fields (DB job passwords, S3 keys,
// proxy password, Telegram bot token) at rest using a locally generated
// AES-256-GCM key.
//
// The key lives in a separate file (dbtool.key, 0600) from the data it
// protects (dbtool.jobs / dbtool.settings). This does not protect against an
// attacker with full read access to the account running dbtool — that
// attacker can read the key file too — but it does protect against the much
// more common accidental-leak scenarios: a config file included in a backup,
// committed to git, or pasted into a support bundle no longer exposes
// credentials in plaintext on its own. Encryption (not just obfuscation) is
// used so leaking only the data file is not sufficient to recover secrets.
//
// Scheduled jobs (cron) must be able to decrypt unattended, so the key
// cannot require a human-entered passphrase; it is generated once and
// persisted locally instead.
package secret

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"dbtool/internal/paths"
)

// keySize is 32 bytes for AES-256.
const keySize = 32

func keyFilePath() (string, error) {
	dir, err := paths.DbtoolDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dbtool.key"), nil
}

// loadOrCreateKey returns the local AES-256 key used to encrypt credential
// fields at rest, generating and persisting a new random one on first use.
func loadOrCreateKey() ([]byte, error) {
	path, err := keyFilePath()
	if err != nil {
		return nil, fmt.Errorf("locate key file: %w", err)
	}

	if data, err := os.ReadFile(path); err == nil && len(data) == keySize {
		return data, nil
	}

	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate encryption key: %w", err)
	}
	if err := os.WriteFile(path, key, 0600); err != nil {
		return nil, fmt.Errorf("write key file %s: %w", path, err)
	}
	return key, nil
}
