package secret

import (
	"bytes"
	"os"
	"testing"
)

// isolate points HOME (and USERPROFILE, for Windows) at a fresh temp dir so
// key generation never touches the real ~/.dbtool on the machine running
// the tests.
func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	isolate(t)

	plaintext := "super-secret-value"
	ciphertext, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if ciphertext == plaintext {
		t.Fatal("ciphertext must not equal plaintext")
	}

	got, err := Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plaintext {
		t.Errorf("got %q, want %q", got, plaintext)
	}
}

func TestDecryptRejectsUnprefixedValue(t *testing.T) {
	isolate(t)

	if _, err := Decrypt("plain-old-password"); err == nil {
		t.Fatal("expected error decrypting a value without the magic prefix")
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	isolate(t)

	ciphertext, err := Encrypt("hunter2")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	tampered := ciphertext + "AA"
	if _, err := Decrypt(tampered); err == nil {
		t.Fatal("expected error decrypting tampered ciphertext")
	}
}

func TestEncryptFieldEmptyStringStaysEmpty(t *testing.T) {
	isolate(t)

	got, err := EncryptField("")
	if err != nil {
		t.Fatalf("EncryptField: %v", err)
	}
	if got != "" {
		t.Errorf("EncryptField(\"\") = %q, want empty", got)
	}
}

func TestDecryptFieldEmptyString(t *testing.T) {
	isolate(t)

	pt, wasEncrypted, err := DecryptField("")
	if err != nil {
		t.Fatalf("DecryptField: %v", err)
	}
	if pt != "" || !wasEncrypted {
		t.Errorf("DecryptField(\"\") = (%q, %v), want (\"\", true)", pt, wasEncrypted)
	}
}

func TestDecryptFieldLegacyPlaintextDetected(t *testing.T) {
	isolate(t)

	pt, wasEncrypted, err := DecryptField("legacy-plaintext-secret")
	if err != nil {
		t.Fatalf("DecryptField: %v", err)
	}
	if wasEncrypted {
		t.Error("expected wasEncrypted=false for legacy plaintext")
	}
	if pt != "legacy-plaintext-secret" {
		t.Errorf("DecryptField legacy value = %q, want unchanged passthrough", pt)
	}
}

func TestDecryptFieldRoundTripThroughEncryptField(t *testing.T) {
	isolate(t)

	stored, err := EncryptField("s3-secret-key-value")
	if err != nil {
		t.Fatalf("EncryptField: %v", err)
	}

	pt, wasEncrypted, err := DecryptField(stored)
	if err != nil {
		t.Fatalf("DecryptField: %v", err)
	}
	if !wasEncrypted {
		t.Error("expected wasEncrypted=true for a freshly encrypted field")
	}
	if pt != "s3-secret-key-value" {
		t.Errorf("got %q, want %q", pt, "s3-secret-key-value")
	}
}

func TestKeyPersistsAcrossCalls(t *testing.T) {
	isolate(t)

	stored, err := EncryptField("value-a")
	if err != nil {
		t.Fatalf("EncryptField: %v", err)
	}

	// A second Encrypt/Decrypt cycle must reuse the same on-disk key.
	pt, _, err := DecryptField(stored)
	if err != nil {
		t.Fatalf("DecryptField with reused key: %v", err)
	}
	if pt != "value-a" {
		t.Errorf("got %q, want %q", pt, "value-a")
	}
}

func TestInvalidKeyIsNotReplaced(t *testing.T) {
	isolate(t)
	path, err := keyFilePath()
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("broken key")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Encrypt("secret"); err == nil {
		t.Fatal("invalid key accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("key was replaced")
	}
}

func TestConcurrentFirstUseSharesOneKey(t *testing.T) {
	isolate(t)
	results := make(chan []byte, 16)
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func() { key, err := loadOrCreateKey(); results <- key; errs <- err }()
	}
	var first []byte
	for i := 0; i < 16; i++ {
		key := <-results
		if first == nil {
			first = key
		}
		if !bytes.Equal(first, key) {
			t.Fatal("concurrent key mismatch")
		}
	}
	for i := 0; i < 16; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

func TestDecryptDoesNotCreateMissingKey(t *testing.T) {
	isolate(t)
	stored, err := Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	path, _ := keyFilePath()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(stored); err == nil {
		t.Fatal("missing key accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("decrypt recreated key: %v", err)
	}
}
