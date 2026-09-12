package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// magicPrefix marks a stored value as ciphertext produced by Encrypt. Its
// presence (or absence) is what lets DecryptField distinguish an already
// -encrypted field from a legacy plaintext value left over from before
// encryption-at-rest was added, enabling transparent migration.
const magicPrefix = "enc:v1:"

// Encrypt returns an opaque, magic-prefixed, base64-encoded ciphertext for
// plaintext using AES-256-GCM with a random nonce.
func Encrypt(plaintext string) (string, error) {
	gcm, err := newGCM()
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return magicPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt reverses Encrypt. value must carry the magicPrefix.
func Decrypt(value string) (string, error) {
	if !strings.HasPrefix(value, magicPrefix) {
		return "", errors.New("value is not an encrypted field")
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, magicPrefix))
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}

	gcm, err := newGCM()
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:nonceSize], raw[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt (wrong key or corrupted data): %w", err)
	}
	return string(plaintext), nil
}

func newGCM() (cipher.AEAD, error) {
	key, err := loadOrCreateKey()
	if err != nil {
		return nil, err
	}
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

// EncryptField encrypts a credential field for storage. Empty strings are
// left empty — there is nothing to protect, and it keeps "unset" visually
// unambiguous in the stored file.
func EncryptField(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	return Encrypt(plaintext)
}

// DecryptField decrypts a credential field read from storage.
//
// wasEncrypted is false only for legacy plaintext values (no magic prefix) —
// callers use this to detect fields that predate encryption-at-rest and
// re-save them (via EncryptField) to migrate transparently. Empty strings
// report wasEncrypted=true since there is nothing to migrate.
func DecryptField(stored string) (plaintext string, wasEncrypted bool, err error) {
	if stored == "" {
		return "", true, nil
	}
	if !strings.HasPrefix(stored, magicPrefix) {
		return stored, false, nil
	}
	pt, err := Decrypt(stored)
	if err != nil {
		return "", true, err
	}
	return pt, true, nil
}
