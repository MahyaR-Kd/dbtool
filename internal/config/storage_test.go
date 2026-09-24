package config

import (
	"bytes"
	"dbtool/internal/types"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptionFailurePreservesAllConfigs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := configFilePath()
	original := []byte("old|localhost|3306|root\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	// A corrupt record fails before prompting or encrypting any entry.
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "dbtool.masterkey"), []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Overwrite([]types.Config{{Name: "old"}, {Name: "new", Password: "new plaintext"}}); err == nil {
		t.Fatal("encryption failure ignored")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("configs changed on failure: %s %v", got, err)
	}
}

func TestTelegramOptOutRoundTripAndLegacyDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Overwrite([]types.Config{{Name: "off", Host: "h", Port: "3306", User: "u", TelegramDisabled: true}}); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if len(got) != 1 || !got[0].TelegramDisabled {
		t.Fatalf("round trip: %+v", got)
	}
	if err := os.WriteFile(configFilePath(), []byte("legacy|h|3306|u\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got = Load()
	if len(got) != 1 || got[0].TelegramDisabled {
		t.Fatalf("legacy config should send by default: %+v", got)
	}
}
