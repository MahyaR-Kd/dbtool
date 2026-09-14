package db

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsMyloaderRestoreCompletedLine guards a real false-failure: myloader
// (like mydumper) increments an internal error counter for any non-fatal
// MySQL warning, and exits non-zero purely based on that counter
// (src/myloader/myloader.c: `exit_code = errors ? EXIT_FAILURE :
// EXIT_SUCCESS;`), the same class of bug as mydumper/mydumper#1300 on the
// dump side. myloader logs "Restore completed" unconditionally as the very
// last thing it does in main(), after schema, data, checksums, and cleanup
// have all finished — so its presence in the captured log distinguishes a
// real restore failure from this false one.
func TestIsMyloaderRestoreCompletedLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{
			name: "real production log line (glib g_message format)",
			line: "** Message: 17:21:18.909: Restore completed",
			want: true,
		},
		{
			name: "exact marker alone",
			line: "Restore completed",
			want: true,
		},
		{
			name: "unrelated schema checksum line",
			line: "** Message: 17:21:18.870: Schema create checksum confirmed for shopping_cart",
			want: false,
		},
		{
			name: "empty line",
			line: "",
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMyloaderRestoreCompletedLine(tc.line); got != tc.want {
				t.Errorf("isMyloaderRestoreCompletedLine(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

// TestApplyMyloaderQuoteCharacterRaceWorkaround guards a genuine, still-open
// myloader race condition (see myloaderRaceWorkaroundFile): a dump whose
// metadata declares double-quoted identifiers can have its first schema
// file validated by a myloader worker thread before a different worker has
// finished parsing that same metadata and updating the quote character
// myloader expects, aborting with "Identifier quote character (`) not
// found...". Creating an empty metadata.partial.0 marker is myloader's own
// (undocumented) trigger to fall back to a single classification thread,
// which eliminates the race.
func TestApplyMyloaderQuoteCharacterRaceWorkaround(t *testing.T) {
	t.Run("creates marker for a DOUBLE_QUOTE dump", func(t *testing.T) {
		dir := t.TempDir()
		writeMetadata(t, dir, "[config]\nquote-character = DOUBLE_QUOTE\n")

		applyMyloaderQuoteCharacterRaceWorkaround(dir)

		assertMarkerExists(t, dir, true)
	})

	t.Run("no-op for a BACKTICK dump", func(t *testing.T) {
		dir := t.TempDir()
		writeMetadata(t, dir, "[config]\nquote-character = BACKTICK\n")

		applyMyloaderQuoteCharacterRaceWorkaround(dir)

		assertMarkerExists(t, dir, false)
	})

	t.Run("no-op with no metadata file", func(t *testing.T) {
		dir := t.TempDir()

		applyMyloaderQuoteCharacterRaceWorkaround(dir)

		assertMarkerExists(t, dir, false)
	})

	t.Run("leaves an existing marker untouched", func(t *testing.T) {
		dir := t.TempDir()
		writeMetadata(t, dir, "[config]\nquote-character = DOUBLE_QUOTE\n")
		markerPath := filepath.Join(dir, myloaderRaceWorkaroundFile)
		if err := os.WriteFile(markerPath, []byte("genuine stream leftover"), 0644); err != nil {
			t.Fatalf("write existing marker: %v", err)
		}

		applyMyloaderQuoteCharacterRaceWorkaround(dir)

		got, err := os.ReadFile(markerPath)
		if err != nil {
			t.Fatalf("read marker after workaround: %v", err)
		}
		if string(got) != "genuine stream leftover" {
			t.Errorf("existing marker was overwritten: got %q", got)
		}
	})
}

func writeMetadata(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "metadata"), []byte(content), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
}

func assertMarkerExists(t *testing.T, dir string, want bool) {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, myloaderRaceWorkaroundFile))
	got := err == nil
	if got != want {
		t.Errorf("marker exists = %v, want %v (stat err: %v)", got, want, err)
	}
}
