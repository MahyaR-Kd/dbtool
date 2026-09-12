package credvault

import (
	"errors"
	"testing"
)

// isolate points HOME (and USERPROFILE, for Windows) at a fresh temp dir so
// the master key record never touches the real ~/.dbtool, and resets the
// in-memory cached key so each test starts as a "fresh process" would.
func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	cacheMu.Lock()
	cachedKey = nil
	cacheMu.Unlock()
	t.Cleanup(func() {
		cacheMu.Lock()
		cachedKey = nil
		cacheMu.Unlock()
	})
}

// fakeReader returns a readPasswordFunc-compatible function that returns
// each string in responses in order, one per call, and fails the test if
// called more times than there are responses.
func fakeReader(t *testing.T, responses ...string) func(string) (string, error) {
	t.Helper()
	i := 0
	return func(prompt string) (string, error) {
		if i >= len(responses) {
			t.Fatalf("readPasswordFunc called more times than expected (prompt: %q)", prompt)
		}
		r := responses[i]
		i++
		return r, nil
	}
}

func withFakeReader(t *testing.T, f func(string) (string, error)) {
	t.Helper()
	old := readPasswordFunc
	readPasswordFunc = f
	t.Cleanup(func() { readPasswordFunc = old })
}

func TestEncryptDecrypt_FirstUseSetsUpMasterPassword(t *testing.T) {
	isolate(t)
	if IsConfigured() {
		t.Fatal("expected not configured before first use")
	}

	withFakeReader(t, fakeReader(t, "hunter2", "hunter2")) // create, confirm

	ct, err := Encrypt("my-db-password")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !IsConfigured() {
		t.Error("expected IsConfigured() to be true after setup")
	}

	pt, err := Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if pt != "my-db-password" {
		t.Errorf("got %q, want %q", pt, "my-db-password")
	}
}

func TestSetup_MismatchedConfirmationRetries(t *testing.T) {
	isolate(t)
	// First attempt: mismatch. Second attempt: match.
	withFakeReader(t, fakeReader(t, "first", "different", "second", "second"))

	_, err := Encrypt("value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !IsConfigured() {
		t.Error("expected setup to complete after the retry")
	}
}

func TestSetup_EmptyPasswordRejected(t *testing.T) {
	isolate(t)
	withFakeReader(t, fakeReader(t, "", "realpass", "realpass"))

	_, err := Encrypt("value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
}

func TestKeyIsCachedForProcessLifetime(t *testing.T) {
	isolate(t)
	withFakeReader(t, fakeReader(t, "mypassword", "mypassword")) // only ONE setup round-trip expected

	if _, err := Encrypt("value1"); err != nil {
		t.Fatalf("Encrypt 1: %v", err)
	}
	// A second Encrypt call in the same process must NOT prompt again —
	// fakeReader will fail the test if it's called a third time.
	if _, err := Encrypt("value2"); err != nil {
		t.Fatalf("Encrypt 2: %v", err)
	}
	if _, err := Decrypt(mustEncrypt(t, "value3")); err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
}

func mustEncrypt(t *testing.T, s string) string {
	t.Helper()
	ct, err := Encrypt(s)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	return ct
}

func TestUnlock_WrongPasswordThenCorrect(t *testing.T) {
	isolate(t)

	// Set up with "correct-password".
	withFakeReader(t, fakeReader(t, "correct-password", "correct-password"))
	ct, err := Encrypt("secret-value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Simulate a new process: drop the cached key.
	cacheMu.Lock()
	cachedKey = nil
	cacheMu.Unlock()

	// Unlock attempt: wrong, wrong, then correct.
	withFakeReader(t, fakeReader(t, "nope", "still-wrong", "correct-password"))
	pt, err := Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt after retries: %v", err)
	}
	if pt != "secret-value" {
		t.Errorf("got %q, want %q", pt, "secret-value")
	}
}

func TestUnlock_AllAttemptsWrongFails(t *testing.T) {
	isolate(t)

	withFakeReader(t, fakeReader(t, "correct-password", "correct-password"))
	ct, err := Encrypt("secret-value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	cacheMu.Lock()
	cachedKey = nil
	cacheMu.Unlock()

	withFakeReader(t, fakeReader(t, "wrong1", "wrong2", "wrong3"))
	if _, err := Decrypt(ct); err == nil {
		t.Fatal("expected an error when all unlock attempts are wrong")
	}
}

func TestEncryptField_EmptyStringIsNoop(t *testing.T) {
	isolate(t)
	// No fake reader installed — this must not prompt at all.
	got, err := EncryptField("")
	if err != nil {
		t.Fatalf("EncryptField: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if IsConfigured() {
		t.Error("EncryptField(\"\") must not trigger master password setup")
	}
}

func TestEncryptField_AlreadyEncryptedIsNoop(t *testing.T) {
	isolate(t)
	withFakeReader(t, fakeReader(t, "pw", "pw"))
	ct, err := Encrypt("value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Re-encrypting an already-encrypted value must be a pure passthrough —
	// no reader call, so this must not hang/fail even with no fake reader
	// installed for a second round.
	withFakeReader(t, func(string) (string, error) {
		t.Fatal("EncryptField must not prompt for an already-encrypted value")
		return "", nil
	})
	got, err := EncryptField(ct)
	if err != nil {
		t.Fatalf("EncryptField: %v", err)
	}
	if got != ct {
		t.Errorf("got %q, want unchanged %q", got, ct)
	}
}

func TestDecrypt_EmptyStringIsNoop(t *testing.T) {
	isolate(t)
	got, err := Decrypt("")
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if IsConfigured() {
		t.Error("Decrypt(\"\") must not trigger master password setup")
	}
}

func TestDecrypt_RejectsValueWithoutPrefix(t *testing.T) {
	isolate(t)
	if _, err := Decrypt("plain-unprefixed-value"); err == nil {
		t.Fatal("expected an error for a value without the magic prefix")
	}
}

func TestReset_ClearsConfiguration(t *testing.T) {
	isolate(t)
	withFakeReader(t, fakeReader(t, "pw", "pw"))
	if _, err := Encrypt("value"); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !IsConfigured() {
		t.Fatal("expected configured after setup")
	}

	if err := Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if IsConfigured() {
		t.Error("expected not configured after Reset")
	}

	// A subsequent Encrypt call must go through setup again, not reuse the
	// old cached key.
	withFakeReader(t, fakeReader(t, "new-pw", "new-pw"))
	if _, err := Encrypt("value2"); err != nil {
		t.Fatalf("Encrypt after reset: %v", err)
	}
}

func TestReaderError_PropagatesWithoutPanicking(t *testing.T) {
	isolate(t)
	wantErr := errors.New("input aborted")
	withFakeReader(t, func(string) (string, error) { return "", wantErr })

	if _, err := Encrypt("value"); !errors.Is(err, wantErr) {
		t.Errorf("got %v, want %v", err, wantErr)
	}
}

func TestRotate_NotConfiguredReturnsError(t *testing.T) {
	isolate(t)
	if _, err := Rotate([]string{"anything"}); err == nil {
		t.Fatal("expected an error rotating with no master password configured")
	}
}

func TestRotate_ReencryptsUnderNewPassword(t *testing.T) {
	isolate(t)

	// Set up under "old-pw" and encrypt two values.
	withFakeReader(t, fakeReader(t, "old-pw", "old-pw"))
	ct1, err := Encrypt("db-password-1")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	ct2, err := Encrypt("db-password-2")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Simulate a fresh process for the rotate call.
	cacheMu.Lock()
	cachedKey = nil
	cacheMu.Unlock()

	// Rotate: verify "old-pw", then set "new-pw" (create + confirm).
	withFakeReader(t, fakeReader(t, "old-pw", "new-pw", "new-pw"))
	rotated, err := Rotate([]string{ct1, "", ct2})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if len(rotated) != 3 {
		t.Fatalf("got %d values, want 3", len(rotated))
	}
	if rotated[1] != "" {
		t.Errorf("empty input at index 1 should stay empty, got %q", rotated[1])
	}
	if rotated[0] == ct1 || rotated[2] == ct2 {
		t.Error("rotated ciphertexts should differ from the originals (different key/nonce)")
	}

	// The old password must no longer work...
	cacheMu.Lock()
	cachedKey = nil
	cacheMu.Unlock()
	withFakeReader(t, fakeReader(t, "old-pw", "old-pw", "old-pw"))
	if _, err := Decrypt(rotated[0]); err == nil {
		t.Fatal("expected decryption with the old password to fail after rotation")
	}

	// ...but the new one must decrypt correctly back to the original plaintext.
	cacheMu.Lock()
	cachedKey = nil
	cacheMu.Unlock()
	withFakeReader(t, fakeReader(t, "new-pw"))
	pt1, err := Decrypt(rotated[0])
	if err != nil {
		t.Fatalf("Decrypt with new password: %v", err)
	}
	if pt1 != "db-password-1" {
		t.Errorf("got %q, want %q", pt1, "db-password-1")
	}

	pt2, err := Decrypt(rotated[2]) // same process, key already cached — no extra prompt
	if err != nil {
		t.Fatalf("Decrypt second value: %v", err)
	}
	if pt2 != "db-password-2" {
		t.Errorf("got %q, want %q", pt2, "db-password-2")
	}
}

func TestRotate_WrongCurrentPasswordFails(t *testing.T) {
	isolate(t)

	withFakeReader(t, fakeReader(t, "correct-pw", "correct-pw"))
	ct, err := Encrypt("value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	cacheMu.Lock()
	cachedKey = nil
	cacheMu.Unlock()

	// All 3 unlock attempts wrong — Rotate must fail without ever reaching
	// the "choose a new password" step (fakeReader would fail the test if
	// asked for more input than provided).
	withFakeReader(t, fakeReader(t, "wrong1", "wrong2", "wrong3"))
	if _, err := Rotate([]string{ct}); err == nil {
		t.Fatal("expected Rotate to fail when the current master password is wrong")
	}
}

func TestRotate_EmptyInputStillChangesPassword(t *testing.T) {
	isolate(t)

	withFakeReader(t, fakeReader(t, "old-pw", "old-pw"))
	if _, err := Encrypt("unrelated-setup-value"); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	cacheMu.Lock()
	cachedKey = nil
	cacheMu.Unlock()

	// Rotate with no values to re-encrypt — should still verify old and set new.
	withFakeReader(t, fakeReader(t, "old-pw", "new-pw", "new-pw"))
	rotated, err := Rotate(nil)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if len(rotated) != 0 {
		t.Errorf("got %d values, want 0", len(rotated))
	}
}
