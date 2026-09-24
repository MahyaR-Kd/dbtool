// Package archivecrypt provides password-based streaming encryption for
// backup archives sent to S3 or Telegram, so a leaked bucket or chat
// doesn't expose the database dump itself. It's independent of dbtool's
// other two encryption schemes (internal/secret's auto-generated key for
// unattended job/S3/proxy/Telegram credentials, and internal/credvault's
// master-password scheme for an optional saved DB password) — this one
// protects the dump's *contents*, using a password the operator chooses
// and must remember themselves; dbtool never stores it in recoverable
// form beyond what's needed to decrypt at rest.
//
// Format: an 8-byte magic/version marker, a 16-byte random salt (fresh
// per encrypted stream), then a sequence of AES-256-GCM-sealed chunks
// (each length-prefixed), ending in an authenticated empty chunk. Legacy v1
// archives remain readable, but lack that completion check. Large archives can be encrypted
// and decrypted without holding the whole file in memory. The key is
// derived from the password and that salt via Argon2id (the same
// parameters internal/credvault uses). Each chunk's nonce is a simple
// monotonic counter, which is safe here specifically because every
// stream gets a fresh random salt and therefore a fresh key — a nonce
// only ever needs to be unique per key, never reused globally.
package archivecrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	magic       = "DBTLENC2"
	legacyMagic = "DBTLENC1"
	saltLen     = 16
	keyLen      = 32 // AES-256
	// chunkSize is the plaintext size per sealed chunk. 1 MiB keeps
	// memory use low for multi-GB dumps while keeping per-chunk framing
	// overhead (4-byte length prefix + 16-byte GCM tag) negligible.
	chunkSize = 1 << 20
	nonceLen  = 12

	// Argon2id parameters, matching internal/credvault so the two
	// password-derived-key schemes in this codebase behave consistently.
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
)

// ErrInvalidFormat means the input doesn't start with archivecrypt's
// magic marker — not an archivecrypt stream at all, or truncated before
// even the header was written.
var ErrInvalidFormat = errors.New("archivecrypt: not a recognized encrypted archive")

// ErrDecryptFailed covers every failure past the header: wrong password
// (GCM authentication fails first, before any plaintext is produced) or
// the stream being corrupted/truncated partway through.
var ErrDecryptFailed = errors.New("archivecrypt: decryption failed (wrong password, or the file is corrupted/truncated)")

func deriveKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, keyLen)
}

func newGCM(password string, salt []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(deriveKey(password, salt))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// chunkNonce derives chunk counter's nonce: 4 zero bytes followed by an
// 8-byte big-endian counter, giving 2^64 possible chunks before any
// repeat — far beyond any real archive size at chunkSize granularity.
func chunkNonce(counter uint64) []byte {
	nonce := make([]byte, nonceLen)
	binary.BigEndian.PutUint64(nonce[nonceLen-8:], counter)
	return nonce
}

// EncryptStream reads plaintext from r and writes the encrypted archive
// format to w, deriving the key from password and a fresh random salt.
func EncryptStream(w io.Writer, r io.Reader, password string) error {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("generate salt: %w", err)
	}

	gcm, err := newGCM(password, salt)
	if err != nil {
		return err
	}

	if _, err := w.Write([]byte(magic)); err != nil {
		return err
	}
	if _, err := w.Write(salt); err != nil {
		return err
	}

	buf := make([]byte, chunkSize)
	var counter uint64
	for {
		n, readErr := io.ReadFull(r, buf)
		if n > 0 {
			ciphertext := gcm.Seal(nil, chunkNonce(counter), buf[:n], []byte(magic))
			var lenBuf [4]byte
			binary.BigEndian.PutUint32(lenBuf[:], uint32(len(ciphertext)))
			if _, err := w.Write(lenBuf[:]); err != nil {
				return err
			}
			if _, err := w.Write(ciphertext); err != nil {
				return err
			}
			counter++
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			// An authenticated empty chunk terminates v2, including empty archives.
			final := gcm.Seal(nil, chunkNonce(counter), nil, []byte(magic))
			var length [4]byte
			binary.BigEndian.PutUint32(length[:], uint32(len(final)))
			if _, err := w.Write(length[:]); err != nil {
				return err
			}
			_, err := w.Write(final)
			return err
		}
		if readErr != nil {
			return readErr
		}
	}
}

// DecryptStream reads the encrypted archive format from r and writes the
// original plaintext to w, deriving the key from password and the salt
// stored in the stream's header.
func DecryptStream(w io.Writer, r io.Reader, password string) error {
	header := make([]byte, len(magic)+saltLen)
	if _, err := io.ReadFull(r, header); err != nil {
		return ErrInvalidFormat
	}
	version := string(header[:len(magic)])
	if version != magic && version != legacyMagic {
		return ErrInvalidFormat
	}
	salt := header[len(magic):]

	gcm, err := newGCM(password, salt)
	if err != nil {
		return err
	}

	var counter uint64
	for {
		var lenBuf [4]byte
		_, err := io.ReadFull(r, lenBuf[:])
		if err == io.EOF {
			if version == legacyMagic {
				return nil
			}
			return ErrDecryptFailed
		}
		if err != nil {
			return ErrDecryptFailed
		}

		length := binary.BigEndian.Uint32(lenBuf[:])
		if length < uint32(gcm.Overhead()) || length > uint32(chunkSize+gcm.Overhead()) {
			return ErrDecryptFailed
		}
		ciphertext := make([]byte, length)
		if _, err := io.ReadFull(r, ciphertext); err != nil {
			return ErrDecryptFailed
		}

		var aad []byte
		if version == magic {
			aad = []byte(magic)
		}
		plaintext, err := gcm.Open(nil, chunkNonce(counter), ciphertext, aad)
		if err != nil {
			return ErrDecryptFailed
		}
		if version == magic && len(plaintext) == 0 {
			var extra [1]byte
			if _, err := io.ReadFull(r, extra[:]); err != io.EOF {
				return ErrDecryptFailed
			}
			return nil
		}
		if _, err := w.Write(plaintext); err != nil {
			return err
		}
		counter++
	}
}
