package archivecrypt

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

func roundTrip(t *testing.T, plaintext []byte, password string) []byte {
	t.Helper()
	var ciphertext bytes.Buffer
	if err := EncryptStream(&ciphertext, bytes.NewReader(plaintext), password); err != nil {
		t.Fatalf("EncryptStream: %v", err)
	}

	var got bytes.Buffer
	if err := DecryptStream(&got, bytes.NewReader(ciphertext.Bytes()), password); err != nil {
		t.Fatalf("DecryptStream: %v", err)
	}
	return got.Bytes()
}

func TestRoundTrip_VariousSizes(t *testing.T) {
	sizes := []int{
		0,
		1,
		100,
		chunkSize - 1,
		chunkSize,
		chunkSize + 1,
		int(2.5 * chunkSize),
	}

	for _, size := range sizes {
		plaintext := make([]byte, size)
		if _, err := rand.Read(plaintext); err != nil {
			t.Fatalf("generate plaintext: %v", err)
		}

		got := roundTrip(t, plaintext, "correct-horse-battery-staple")
		if !bytes.Equal(got, plaintext) {
			t.Errorf("size %d: round-tripped data does not match original (got %d bytes, want %d)", size, len(got), len(plaintext))
		}
	}
}

func TestDecrypt_WrongPasswordFails(t *testing.T) {
	var ciphertext bytes.Buffer
	if err := EncryptStream(&ciphertext, bytes.NewReader([]byte("some dump data")), "correct-password"); err != nil {
		t.Fatalf("EncryptStream: %v", err)
	}

	var out bytes.Buffer
	err := DecryptStream(&out, bytes.NewReader(ciphertext.Bytes()), "wrong-password")
	if !errors.Is(err, ErrDecryptFailed) {
		t.Errorf("got err=%v, want ErrDecryptFailed", err)
	}
}

func TestDecrypt_NotAnArchivecryptStream(t *testing.T) {
	err := DecryptStream(io.Discard, bytes.NewReader([]byte("just some random plain text, not encrypted at all")), "any-password")
	if !errors.Is(err, ErrInvalidFormat) {
		t.Errorf("got err=%v, want ErrInvalidFormat", err)
	}
}

func TestDecrypt_EmptyInput(t *testing.T) {
	err := DecryptStream(io.Discard, bytes.NewReader(nil), "any-password")
	if !errors.Is(err, ErrInvalidFormat) {
		t.Errorf("got err=%v, want ErrInvalidFormat", err)
	}
}

func TestDecrypt_TruncatedStreamFailsCleanly(t *testing.T) {
	plaintext := make([]byte, chunkSize+1000)
	if _, err := rand.Read(plaintext); err != nil {
		t.Fatalf("generate plaintext: %v", err)
	}

	var ciphertext bytes.Buffer
	if err := EncryptStream(&ciphertext, bytes.NewReader(plaintext), "pw"); err != nil {
		t.Fatalf("EncryptStream: %v", err)
	}

	truncated := ciphertext.Bytes()[:ciphertext.Len()-10]
	err := DecryptStream(io.Discard, bytes.NewReader(truncated), "pw")
	if !errors.Is(err, ErrDecryptFailed) {
		t.Errorf("got err=%v, want ErrDecryptFailed", err)
	}
}

func TestDecrypt_TamperedCiphertextDetected(t *testing.T) {
	var ciphertext bytes.Buffer
	if err := EncryptStream(&ciphertext, bytes.NewReader([]byte("sensitive dump bytes")), "pw"); err != nil {
		t.Fatalf("EncryptStream: %v", err)
	}

	tampered := ciphertext.Bytes()
	// Flip a bit well past the header, inside the sealed chunk.
	tampered[len(tampered)-1] ^= 0xFF

	err := DecryptStream(io.Discard, bytes.NewReader(tampered), "pw")
	if !errors.Is(err, ErrDecryptFailed) {
		t.Errorf("got err=%v, want ErrDecryptFailed (GCM should reject tampered ciphertext)", err)
	}
}

func TestEncrypt_SamePlaintextProducesDifferentCiphertext(t *testing.T) {
	plaintext := []byte("identical input, should not leak via ciphertext equality")

	var a, b bytes.Buffer
	if err := EncryptStream(&a, bytes.NewReader(plaintext), "pw"); err != nil {
		t.Fatalf("EncryptStream a: %v", err)
	}
	if err := EncryptStream(&b, bytes.NewReader(plaintext), "pw"); err != nil {
		t.Fatalf("EncryptStream b: %v", err)
	}

	if bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("two encryptions of the same plaintext under the same password produced identical ciphertext (salt/nonce not varying per stream)")
	}
}

func TestDecryptRejectsTruncationAtChunkBoundary(t *testing.T) {
	var encrypted bytes.Buffer
	if err := EncryptStream(&encrypted, bytes.NewReader(bytes.Repeat([]byte("x"), chunkSize+100)), "pw"); err != nil {
		t.Fatal(err)
	}
	data := encrypted.Bytes()
	end := len(magic) + saltLen + 4 + chunkSize + 16
	if err := DecryptStream(io.Discard, bytes.NewReader(data[:end]), "pw"); !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("truncated archive accepted: %v", err)
	}
	if err := DecryptStream(io.Discard, bytes.NewReader(data[:len(magic)+saltLen]), "pw"); !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("header-only archive accepted: %v", err)
	}
}

func TestDecryptRejectsOversizedChunkAndTrailingData(t *testing.T) {
	var encrypted bytes.Buffer
	if err := EncryptStream(&encrypted, bytes.NewReader(nil), "pw"); err != nil {
		t.Fatal(err)
	}
	data := append([]byte(nil), encrypted.Bytes()...)
	for i := len(magic) + saltLen; i < len(magic)+saltLen+4; i++ {
		data[i] = 0xff
	}
	if err := DecryptStream(io.Discard, bytes.NewReader(data), "pw"); !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("oversized chunk accepted: %v", err)
	}
	data = append(encrypted.Bytes(), 1)
	if err := DecryptStream(io.Discard, bytes.NewReader(data), "pw"); !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("trailing data accepted: %v", err)
	}
}

func TestLegacyArchiveRemainsReadable(t *testing.T) {
	salt := make([]byte, saltLen)
	gcm, err := newGCM("pw", salt)
	if err != nil {
		t.Fatal(err)
	}
	sealed := gcm.Seal(nil, chunkNonce(0), []byte("old backup"), nil)
	var encrypted bytes.Buffer
	encrypted.WriteString(legacyMagic)
	encrypted.Write(salt)
	encrypted.Write([]byte{0, 0, 0, byte(len(sealed))})
	encrypted.Write(sealed)
	var out bytes.Buffer
	if err := DecryptStream(&out, bytes.NewReader(encrypted.Bytes()), "pw"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "old backup" {
		t.Fatal(out.String())
	}
}
