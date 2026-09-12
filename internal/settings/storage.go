package settings

import (
	"encoding/json"
	"os"
	"path/filepath"

	"dbtool/internal/logger"
	"dbtool/internal/paths"
	"dbtool/internal/secret"
)

func settingsFilePath() string {
	dir, err := paths.DbtoolDir()
	if err != nil {
		// fallback: use home dir directly
		return filepath.Join(os.ExpandEnv("$HOME"), ".dbtool", "dbtool.settings")
	}
	return filepath.Join(dir, "dbtool.settings")
}

// defaultWorkDir returns ~/dbtool-backups.
func defaultWorkDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "dbtool-backups"
	}
	return filepath.Join(home, "dbtool-backups")
}

// Load reads the settings file and returns a Settings struct with credential
// fields (S3 keys, proxy password, Telegram bot token) decrypted, ready to
// use. Missing or unset fields are filled with sensible defaults.
//
// Credential fields found in legacy plaintext form (from installs predating
// encryption-at-rest) are transparently re-saved encrypted.
func Load() Settings {
	data, err := os.ReadFile(settingsFilePath())
	if err != nil {
		return Settings{WorkDir: defaultWorkDir()}
	}

	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{WorkDir: defaultWorkDir()}
	}

	if s.WorkDir == "" {
		s.WorkDir = defaultWorkDir()
	}

	if s.StorageType == "" {
		s.StorageType = StorageLocal
	}

	needsMigration := false
	decrypt := func(field, value string) string {
		pt, wasEncrypted, err := secret.DecryptField(value)
		if err != nil {
			logger.Error("settings: failed to decrypt %s, treating as unset: %v", field, err)
			return ""
		}
		if !wasEncrypted {
			needsMigration = true
		}
		return pt
	}

	s.S3.AccessKey = decrypt("s3.access_key", s.S3.AccessKey)
	s.S3.SecretKey = decrypt("s3.secret_key", s.S3.SecretKey)
	s.Proxy.Password = decrypt("proxy.password", s.Proxy.Password)
	s.Telegram.BotToken = decrypt("telegram.bot_token", s.Telegram.BotToken)

	if needsMigration {
		logger.Info("settings: migrating plaintext credentials to encrypted storage")
		if err := Save(s); err != nil {
			logger.Error("settings: failed to migrate credentials to encrypted storage: %v", err)
		}
	}

	return s
}

// Save writes the settings to disk, encrypting credential fields (S3 keys,
// proxy password, Telegram bot token) before they touch disk. The caller's
// in-memory Settings value is left untouched — only the on-disk copy is
// affected — so callers can keep using s normally after Save returns.
func Save(s Settings) error {
	out := s

	var err error
	if out.S3.AccessKey, err = secret.EncryptField(s.S3.AccessKey); err != nil {
		return err
	}
	if out.S3.SecretKey, err = secret.EncryptField(s.S3.SecretKey); err != nil {
		return err
	}
	if out.Proxy.Password, err = secret.EncryptField(s.Proxy.Password); err != nil {
		return err
	}
	if out.Telegram.BotToken, err = secret.EncryptField(s.Telegram.BotToken); err != nil {
		return err
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	path := settingsFilePath()
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	// os.WriteFile only applies the mode argument when creating a new file;
	// for a pre-existing file (e.g. one left at the old 0644 from before
	// credentials were stored here) it leaves permissions untouched. Chmod
	// explicitly so upgrades always end up at 0600, not just fresh installs.
	return os.Chmod(path, 0600)
}
