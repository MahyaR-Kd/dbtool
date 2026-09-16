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

// TestIsMyloaderReadOnlyErrorLine guards the trigger for RunRestore's
// wait-and-retry path: a destination that's briefly read-only (e.g. a
// managed-MySQL failover or maintenance event) makes myloader abort with
// this exact MySQL error text (ERROR 1290), which is otherwise
// indistinguishable from any other fatal restore error without matching on
// it specifically.
func TestIsMyloaderReadOnlyErrorLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{
			name: "real production log line",
			line: "** (myloader:1090237): CRITICAL **: 09:58:08.075: Thread 2 using connection 194933 - ERROR 1290: Error occurs between lines: 1947 and 2103: The MySQL server is running with the --read-only option so it cannot execute this statement",
			want: true,
		},
		{
			name: "unrelated critical error",
			line: "** (myloader:1090237): CRITICAL **: Thread 1 using connection 1 - ERROR 1146: Table 'foo.bar' doesn't exist",
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
			if got := isMyloaderReadOnlyErrorLine(tc.line); got != tc.want {
				t.Errorf("isMyloaderReadOnlyErrorLine(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

// TestListDumpDatabases guards the granularity RunRestore retries at: one
// myloader invocation per source database (see restoreOneDatabase), so a
// read-only failure well into a restore only has to redo the one database
// in progress, not tables already safely committed in earlier ones.
func TestListDumpDatabases(t *testing.T) {
	t.Run("finds databases across compression formats", func(t *testing.T) {
		dir := t.TempDir()
		touchFiles(t, dir,
			"accounting-schema-create.sql",
			"accounting.credit_increases-schema.sql.zst",
			"accounting.credit_increases.00000.sql.zst",
			"orders-schema-create.sql.gz",
			"orders.order_shipment_notes-schema.sql.gz",
			"cms-schema-create.sql.zst",
			"metadata",
		)

		got := listDumpDatabases(dir)
		want := []string{"accounting", "cms", "orders"}
		if !slicesEqual(got, want) {
			t.Errorf("listDumpDatabases() = %v, want %v", got, want)
		}
	})

	t.Run("no schema-create files returns nil", func(t *testing.T) {
		dir := t.TempDir()
		touchFiles(t, dir, "accounting.credit_increases-schema.sql", "metadata")

		if got := listDumpDatabases(dir); got != nil {
			t.Errorf("listDumpDatabases() = %v, want nil", got)
		}
	})

	t.Run("empty directory returns nil", func(t *testing.T) {
		if got := listDumpDatabases(t.TempDir()); got != nil {
			t.Errorf("listDumpDatabases() = %v, want nil", got)
		}
	})
}

// TestCountRestoredTables guards the per-database table count RunRestore
// sizes each database's progress bar with.
func TestCountRestoredTables(t *testing.T) {
	dir := t.TempDir()
	touchFiles(t, dir,
		"accounting.credit_increases-schema.sql.zst",
		"accounting.invoice_adjustments-schema.sql.zst",
		"orders.order_shipment_notes-schema.sql.gz",
		"accounting-schema-create.sql",
	)

	if got := countRestoredTables(dir, ""); got != 3 {
		t.Errorf("countRestoredTables(dir, \"\") = %d, want 3", got)
	}
	if got := countRestoredTables(dir, "accounting"); got != 2 {
		t.Errorf("countRestoredTables(dir, \"accounting\") = %d, want 2", got)
	}
	if got := countRestoredTables(dir, "orders"); got != 1 {
		t.Errorf("countRestoredTables(dir, \"orders\") = %d, want 1", got)
	}
	if got := countRestoredTables(dir, "nope"); got != 0 {
		t.Errorf("countRestoredTables(dir, \"nope\") = %d, want 0", got)
	}
}

func touchFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0644); err != nil {
			t.Fatalf("touch %s: %v", name, err)
		}
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
