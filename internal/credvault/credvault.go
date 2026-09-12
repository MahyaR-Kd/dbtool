// Package credvault protects saved DB connection-config passwords with a
// user-chosen master password, as opposed to the auto-generated key used by
// internal/secret for credentials that scheduled/cron jobs must be able to
// decrypt unattended (job passwords, S3 keys, the proxy password, the
// Telegram bot token — none of that changes; this package only covers the
// new, optional password field on a saved dump/restore config).
//
// The master password is never written to disk anywhere — only a
// verification record (a random salt plus a value encrypted under the
// password-derived key) is persisted, so a later unlock attempt can be
// checked without ever storing the password or the key itself. If it's
// forgotten, any passwords already encrypted under it are unrecoverable by
// design; internal/config.ResetMasterPassword exists for that recovery
// path (clears the verification record and every stored config password).
//
// The derived key is cached in memory for the lifetime of the current
// process only (never persisted, never shared across separate dbtool
// invocations) so a single dump/restore run prompts for the master
// password at most once even if it needs to decrypt more than one field.
package credvault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"

	"dbtool/internal/paths"
	"dbtool/internal/secureinput"
)

const magicPrefix = "cred:v1:"

// checkPlaintext is encrypted under the derived key at setup time and
// decrypted at unlock time; success (both AES-GCM authentication passing
// and the plaintext matching) is how a candidate master password is
// verified without ever storing the password or key themselves.
const checkPlaintext = "dbtool-master-password-check-v1"

const (
	keyLen  = 32 // AES-256
	saltLen = 16

	// Argon2id parameters. 64 MiB / 1 pass / 4 threads is OWASP's current
	// baseline recommendation for interactive password verification.
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
)

// readPasswordFunc is indirected so tests can supply canned input instead
// of a real terminal prompt.
var readPasswordFunc = secureinput.ReadPassword

type verificationRecord struct {
	Salt  string `json:"salt"`  // base64
	Check string `json:"check"` // magicPrefix-tagged ciphertext of checkPlaintext
}

func masterKeyFilePath() (string, error) {
	dir, err := paths.DbtoolDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dbtool.masterkey"), nil
}

var (
	cacheMu   sync.Mutex
	cachedKey []byte
)

// IsConfigured reports whether a master password has already been set up.
func IsConfigured() bool {
	path, err := masterKeyFilePath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Reset permanently discards the master password's verification record.
// Any config passwords already encrypted under the old master password
// become permanently undecryptable — callers are expected to also clear
// those fields (see internal/config.ResetMasterPassword, which does both).
func Reset() error {
	path, err := masterKeyFilePath()
	if err != nil {
		return err
	}
	cacheMu.Lock()
	cachedKey = nil
	cacheMu.Unlock()
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Rotate re-encrypts every value in encryptedValues (each already encrypted
// under the current master password, or "" for "nothing to re-encrypt at
// this position") under a newly-chosen master password, replacing the
// on-disk verification record. It prompts for the current master password
// exactly once (to verify it and derive the decrypt key — this is a
// deliberate re-confirmation, not reused from any cached key, the same way
// a passwd-style "change password" flow always re-asks the current one)
// and then a new one exactly once (create + confirm).
//
// Returns the re-encrypted values in the same order and positions as
// encryptedValues. If it returns an error, the verification record may
// already have been replaced (setup() persists as its last step and
// essentially cannot fail afterward) but none of the returned values
// should be trusted — callers must not persist anything from a
// non-nil-error return.
func Rotate(encryptedValues []string) ([]string, error) {
	if !IsConfigured() {
		return nil, errors.New("no master password is set up yet — nothing to change")
	}

	oldKey, err := unlock()
	if err != nil {
		return nil, fmt.Errorf("verify current master password: %w", err)
	}

	plaintexts := make([]string, len(encryptedValues))
	for i, ct := range encryptedValues {
		if ct == "" {
			continue
		}
		pt, err := decryptWithKey(oldKey, ct)
		if err != nil {
			return nil, fmt.Errorf("decrypt value %d under current master password: %w", i, err)
		}
		plaintexts[i] = pt
	}

	fmt.Println("Current master password verified. Choose a new one.")
	newKey, err := createNewPassword()
	if err != nil {
		return nil, fmt.Errorf("set new master password: %w", err)
	}

	cacheMu.Lock()
	cachedKey = newKey
	cacheMu.Unlock()

	out := make([]string, len(plaintexts))
	for i, pt := range plaintexts {
		if pt == "" {
			continue
		}
		ct, err := encryptWithKey(newKey, pt)
		if err != nil {
			return nil, fmt.Errorf("re-encrypt value %d under new master password: %w", i, err)
		}
		out[i] = ct
	}
	return out, nil
}

func deriveKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, keyLen)
}

// setup prints first-time-setup context, then delegates to
// createNewPassword. Used by getKey() when no master password exists yet.
func setup() ([]byte, error) {
	fmt.Println("No master password is set up yet for storing DB config passwords.")
	fmt.Println("It is never stored anywhere — only you know it. If you forget it, any")
	fmt.Println("passwords already saved under it cannot be recovered and must be re-entered.")
	return createNewPassword()
}

// createNewPassword prompts for a new master password (entered twice for
// confirmation), persists its verification record — replacing any existing
// one — and returns the derived key. Callers are responsible for printing
// any context-appropriate message beforehand (first-time setup vs. a
// deliberate change of an existing password read very differently).
func createNewPassword() ([]byte, error) {
	for {
		p1, err := readPasswordFunc("Create a master password: ")
		if err != nil {
			return nil, err
		}
		if p1 == "" {
			fmt.Println("Master password cannot be empty.")
			continue
		}
		p2, err := readPasswordFunc("Confirm master password: ")
		if err != nil {
			return nil, err
		}
		if p1 != p2 {
			fmt.Println("Passwords did not match. Try again.")
			continue
		}

		salt := make([]byte, saltLen)
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return nil, fmt.Errorf("generate salt: %w", err)
		}
		key := deriveKey(p1, salt)

		checkCT, err := encryptWithKey(key, checkPlaintext)
		if err != nil {
			return nil, err
		}

		if err := persist(verificationRecord{
			Salt:  base64.StdEncoding.EncodeToString(salt),
			Check: checkCT,
		}); err != nil {
			return nil, err
		}

		fmt.Println("Master password set.")
		return key, nil
	}
}

func persist(rec verificationRecord) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	path, err := masterKeyFilePath()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	// WriteFile's mode argument only applies when creating a new file; chmod
	// explicitly so a pre-existing file (e.g. left over from a Reset that
	// failed partway) ends up 0600 regardless.
	return os.Chmod(path, 0600)
}

func loadRecord() (verificationRecord, []byte, error) {
	path, err := masterKeyFilePath()
	if err != nil {
		return verificationRecord{}, nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return verificationRecord{}, nil, fmt.Errorf("read master password record: %w", err)
	}
	var rec verificationRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return verificationRecord{}, nil, fmt.Errorf("parse master password record: %w", err)
	}
	salt, err := base64.StdEncoding.DecodeString(rec.Salt)
	if err != nil {
		return verificationRecord{}, nil, fmt.Errorf("decode salt: %w", err)
	}
	return rec, salt, nil
}

// unlock prompts for the master password (up to a few attempts on a
// mismatch) and returns the derived key once verified.
func unlock() ([]byte, error) {
	rec, salt, err := loadRecord()
	if err != nil {
		return nil, err
	}

	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		pass, err := readPasswordFunc("Master password: ")
		if err != nil {
			return nil, err
		}
		key := deriveKey(pass, salt)
		if pt, err := decryptWithKey(key, rec.Check); err == nil && pt == checkPlaintext {
			return key, nil
		}
		if attempt < maxAttempts {
			fmt.Println("Incorrect master password, try again.")
		}
	}
	return nil, errors.New("incorrect master password")
}

// getKey returns the derived key for this process, unlocking (or setting
// up, on first-ever use) as needed. Prompts at most once per process.
func getKey() ([]byte, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cachedKey != nil {
		return cachedKey, nil
	}

	var key []byte
	var err error
	if IsConfigured() {
		key, err = unlock()
	} else {
		key, err = setup()
	}
	if err != nil {
		return nil, err
	}
	cachedKey = key
	return key, nil
}

// Encrypt encrypts plaintext for storage, prompting for the master
// password (or setting one up, on first use) as needed. Empty strings are
// left empty.
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	key, err := getKey()
	if err != nil {
		return "", err
	}
	return encryptWithKey(key, plaintext)
}

// EncryptField is like Encrypt but is a no-op for a value that's already
// encrypted (carries the magic prefix) or empty — safe to call on a config
// whose Password field might be a fresh plaintext value the user just
// typed, or an already-encrypted value carried over unmodified from a
// previous load, without double-encrypting the latter.
func EncryptField(value string) (string, error) {
	if value == "" || strings.HasPrefix(value, magicPrefix) {
		return value, nil
	}
	return Encrypt(value)
}

// Decrypt decrypts a value produced by Encrypt/EncryptField, prompting for
// the master password as needed. Empty strings decrypt to empty strings.
func Decrypt(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if !strings.HasPrefix(stored, magicPrefix) {
		return "", errors.New("value is not a master-password-protected field")
	}
	key, err := getKey()
	if err != nil {
		return "", err
	}
	return decryptWithKey(key, stored)
}

func encryptWithKey(key []byte, plaintext string) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return magicPrefix + base64.StdEncoding.EncodeToString(ct), nil
}

func decryptWithKey(key []byte, stored string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, magicPrefix))
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt (wrong master password or corrupted data): %w", err)
	}
	return string(pt), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("init cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("init GCM: %w", err)
	}
	return gcm, nil
}
