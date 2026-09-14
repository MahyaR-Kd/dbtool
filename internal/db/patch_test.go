package db

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestPatchSQL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "NOT NULL datetime default → nullable NULL default",
			input: "  `created_at` datetime NOT NULL DEFAULT '0000-00-00 00:00:00',",
			want:  "  `created_at` datetime NULL DEFAULT NULL,",
		},
		{
			name:  "NOT NULL date default → nullable NULL default",
			input: "  `birth_date` date NOT NULL DEFAULT '0000-00-00',",
			want:  "  `birth_date` date NULL DEFAULT NULL,",
		},
		{
			name:  "nullable column with zero datetime default",
			input: "  `updated_at` datetime DEFAULT '0000-00-00 00:00:00',",
			want:  "  `updated_at` datetime DEFAULT NULL,",
		},
		{
			name:  "nullable column with zero date default",
			input: "  `expiry` date DEFAULT '0000-00-00',",
			want:  "  `expiry` date DEFAULT NULL,",
		},
		{
			name:  "NOT NULL timestamp with ON UPDATE clause",
			input: "  `ts` timestamp NOT NULL DEFAULT '0000-00-00 00:00:00' ON UPDATE CURRENT_TIMESTAMP,",
			want:  "  `ts` timestamp NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,",
		},
		{
			name:  "normal NOT NULL with real default — untouched",
			input: "  `status` tinyint(1) NOT NULL DEFAULT '1',",
			want:  "  `status` tinyint(1) NOT NULL DEFAULT '1',",
		},
		{
			name:  "normal nullable with real default — untouched",
			input: "  `note` varchar(255) DEFAULT 'active',",
			want:  "  `note` varchar(255) DEFAULT 'active',",
		},
		{
			name:  "NOT NULL without default — untouched",
			input: "  `id` int NOT NULL AUTO_INCREMENT,",
			want:  "  `id` int NOT NULL AUTO_INCREMENT,",
		},
		{
			name: "multiple columns in one SQL block",
			input: "CREATE TABLE `t` (\n" +
				"  `id` int NOT NULL,\n" +
				"  `created_at` datetime NOT NULL DEFAULT '0000-00-00 00:00:00',\n" +
				"  `updated_at` datetime DEFAULT '0000-00-00 00:00:00',\n" +
				"  `name` varchar(50) NOT NULL DEFAULT 'unknown'\n" +
				");",
			want: "CREATE TABLE `t` (\n" +
				"  `id` int NOT NULL,\n" +
				"  `created_at` datetime NULL DEFAULT NULL,\n" +
				"  `updated_at` datetime DEFAULT NULL,\n" +
				"  `name` varchar(50) NOT NULL DEFAULT 'unknown'\n" +
				");",
		},
		{
			name:  "empty string — no change",
			input: "",
			want:  "",
		},
		{
			name:  "nullable date column with current_timestamp default",
			input: "  `start_date` date DEFAULT current_timestamp(),",
			want:  "  `start_date` date DEFAULT NULL,",
		},
		{
			name:  "nullable date column with CURRENT_TIMESTAMP (no parens)",
			input: "  `end_date` date DEFAULT CURRENT_TIMESTAMP,",
			want:  "  `end_date` date DEFAULT NULL,",
		},
		{
			name:  "NOT NULL date column with current_timestamp default → nullable NULL default",
			input: "  `start_date` date NOT NULL DEFAULT current_timestamp(),",
			want:  "  `start_date` date NULL DEFAULT NULL,",
		},
		{
			name:  "datetime column with current_timestamp — untouched",
			input: "  `created_at` datetime NOT NULL DEFAULT current_timestamp(),",
			want:  "  `created_at` datetime NOT NULL DEFAULT current_timestamp(),",
		},
		{
			name:  "timestamp column with current_timestamp — untouched",
			input: "  `updated_at` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),",
			want:  "  `updated_at` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),",
		},
		{
			name: "store_settings-style table with date columns using current_timestamp",
			input: "CREATE TABLE `store_settings` (\n" +
				"  `id` tinyint(3) unsigned NOT NULL AUTO_INCREMENT,\n" +
				"  `start_date` date DEFAULT current_timestamp(),\n" +
				"  `end_date` date DEFAULT current_timestamp(),\n" +
				"  `created_at` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),\n" +
				"  `updated_at` timestamp NOT NULL DEFAULT current_timestamp(),\n" +
				"  PRIMARY KEY (`id`)\n" +
				");",
			want: "CREATE TABLE `store_settings` (\n" +
				"  `id` tinyint(3) unsigned NOT NULL AUTO_INCREMENT,\n" +
				"  `start_date` date DEFAULT NULL,\n" +
				"  `end_date` date DEFAULT NULL,\n" +
				"  `created_at` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),\n" +
				"  `updated_at` timestamp NOT NULL DEFAULT current_timestamp(),\n" +
				"  PRIMARY KEY (`id`)\n" +
				");",
		},
		{
			name: "ANSI-quoted identifiers become MySQL backticks",
			input: "CREATE TABLE \"catalog\".\"product_sort\" (\n" +
				"  \"id\" int NOT NULL,\n" +
				"  \"note\" varchar(30) DEFAULT 'a \"quoted\" value',\n" +
				"  PRIMARY KEY (\"id\")\n" +
				");",
			want: "CREATE TABLE `catalog`.`product_sort` (\n" +
				"  `id` int NOT NULL,\n" +
				"  `note` varchar(30) DEFAULT 'a \"quoted\" value',\n" +
				"  PRIMARY KEY (`id`)\n" +
				");",
		},
		{
			name:  "double quotes without ANSI CREATE TABLE are untouched",
			input: "INSERT INTO `notes` VALUES ('a \"quoted\" value');",
			want:  "INSERT INTO `notes` VALUES ('a \"quoted\" value');",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := patchSQL(tc.input, true)
			if got != tc.want {
				t.Errorf("\ninput: %q\n  got: %q\n want: %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestPatchDumpDir_GzFile(t *testing.T) {
	dir := t.TempDir()

	schemaSQL := "CREATE TABLE `orders` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `placed_at` datetime NOT NULL DEFAULT '0000-00-00 00:00:00'\n" +
		");\n"
	wantSQL := "CREATE TABLE `orders` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `placed_at` datetime NULL DEFAULT NULL\n" +
		");\n"

	// Write a compressed schema file with a zero-date default.
	gzPath := filepath.Join(dir, "mydb.orders-schema.sql.gz")
	f, err := os.Create(gzPath)
	if err != nil {
		t.Fatalf("create gz: %v", err)
	}
	gw := gzip.NewWriter(f)
	gw.Write([]byte(schemaSQL))
	gw.Close()
	f.Close()

	PatchDumpDir(dir)

	// Read and decompress the patched file.
	f2, err := os.Open(gzPath)
	if err != nil {
		t.Fatalf("open gz after patch: %v", err)
	}
	defer f2.Close()
	gr, err := gzip.NewReader(f2)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gr.Close()
	raw, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("read patched gz: %v", err)
	}
	if string(raw) != wantSQL {
		t.Errorf("patched content mismatch\n got: %q\nwant: %q", string(raw), wantSQL)
	}
}

// TestPatchDumpDir_ZstFile guards against a real bug: mydumper's --compress
// flag can produce .zst (zstd) output instead of .gz depending on version —
// a mydumper 1.0.5 build was observed doing exactly this. Before
// detectCompression/openCompressed/writeCompressed existed, patchSchemaFile
// only recognized ".gz", so a .zst schema file was read as raw compressed
// bytes, patchSQL's regexes matched nothing against that garbage, and the
// function silently no-opped — the ANSI-quote-to-backtick conversion never
// ran, and myloader failed with "Identifier quote character (`) not found".
func TestPatchDumpDir_ZstFile(t *testing.T) {
	dir := t.TempDir()

	schemaSQL := "CREATE TABLE `orders` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `placed_at` datetime NOT NULL DEFAULT '0000-00-00 00:00:00'\n" +
		");\n"
	wantSQL := "CREATE TABLE `orders` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `placed_at` datetime NULL DEFAULT NULL\n" +
		");\n"

	zstPath := filepath.Join(dir, "mydb.orders-schema.sql.zst")
	f, err := os.Create(zstPath)
	if err != nil {
		t.Fatalf("create zst: %v", err)
	}
	zw, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatalf("zstd.NewWriter: %v", err)
	}
	if _, err := zw.Write([]byte(schemaSQL)); err != nil {
		t.Fatalf("write zst: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zstd writer: %v", err)
	}
	f.Close()

	PatchDumpDir(dir)

	f2, err := os.Open(zstPath)
	if err != nil {
		t.Fatalf("open zst after patch: %v", err)
	}
	defer f2.Close()
	zr, err := zstd.NewReader(f2)
	if err != nil {
		t.Fatalf("zstd.NewReader: %v", err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read patched zst: %v", err)
	}
	if string(raw) != wantSQL {
		t.Errorf("patched content mismatch\n got: %q\nwant: %q", string(raw), wantSQL)
	}
}

// TestPatchDumpDir_SkipsAnsiQuoteConversionForMydumper1x guards a real
// bug: mydumper 1.x auto-detects ANSI_QUOTES mode on the source, writes
// every file in the dump using that quoting consistently, and records the
// choice in the dump's own metadata file ("[config]\nquote-character =
// DOUBLE_QUOTE"). myloader 1.x reads that back and enforces it against
// every file. Before this fix, dbtool unconditionally converted
// double-quoted identifiers to backticks — which broke that contract:
// myloader would look for a double quote (per metadata) and find a
// backtick instead (because dbtool rewrote it), and abort with
// "Identifier quote character (\") not found". A dump with that metadata
// key must be left with its original quoting untouched.
func TestPatchDumpDir_SkipsAnsiQuoteConversionForMydumper1x(t *testing.T) {
	dir := t.TempDir()

	schemaSQL := `CREATE TABLE "orders" (` + "\n" +
		`  "id" int NOT NULL` + "\n" +
		`);` + "\n"

	if err := os.WriteFile(filepath.Join(dir, "mydb.orders-schema.sql"), []byte(schemaSQL), 0644); err != nil {
		t.Fatalf("write schema file: %v", err)
	}
	// A mydumper 1.x-style metadata file recording ANSI-quote mode.
	metadata := "[config]\nquote-character = DOUBLE_QUOTE\n"
	if err := os.WriteFile(filepath.Join(dir, "metadata"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	PatchDumpDir(dir)

	got, err := os.ReadFile(filepath.Join(dir, "mydb.orders-schema.sql"))
	if err != nil {
		t.Fatalf("read schema file after patch: %v", err)
	}
	if string(got) != schemaSQL {
		t.Errorf("schema file was modified despite mydumper 1.x metadata\n got: %q\nwant (unchanged): %q", got, schemaSQL)
	}
}

// TestPatchDumpDir_ConvertsAnsiQuotesWithoutMydumper1xMetadata is the
// converse of the test above: with no metadata file at all (or one
// without a quote-character key — an older mydumper's dump), the ANSI
// double-quote-to-backtick conversion must still run, since old myloader
// always expects backticks and has no metadata-driven auto-detection.
func TestPatchDumpDir_ConvertsAnsiQuotesWithoutMydumper1xMetadata(t *testing.T) {
	dir := t.TempDir()

	schemaSQL := `CREATE TABLE "orders" (` + "\n" +
		`  "id" int NOT NULL` + "\n" +
		`);` + "\n"
	wantSQL := "CREATE TABLE `orders` (\n" +
		"  `id` int NOT NULL\n" +
		");\n"

	if err := os.WriteFile(filepath.Join(dir, "mydb.orders-schema.sql"), []byte(schemaSQL), 0644); err != nil {
		t.Fatalf("write schema file: %v", err)
	}

	PatchDumpDir(dir)

	got, err := os.ReadFile(filepath.Join(dir, "mydb.orders-schema.sql"))
	if err != nil {
		t.Fatalf("read schema file after patch: %v", err)
	}
	if string(got) != wantSQL {
		t.Errorf("got: %q\nwant: %q", got, wantSQL)
	}
}

// TestDumpMetadataQuoteCharacterIsDoubleQuote guards the detection used by
// RunRestore to decide whether a dump is at risk of the myloader
// quote-character race (see myloaderRaceWorkaroundFile in restore.go).
func TestDumpMetadataQuoteCharacterIsDoubleQuote(t *testing.T) {
	tests := []struct {
		name     string
		metadata string
		writeIt  bool
		want     bool
	}{
		{
			name:     "declares DOUBLE_QUOTE",
			metadata: "[config]\nquote-character = DOUBLE_QUOTE\n",
			writeIt:  true,
			want:     true,
		},
		{
			name:     "declares BACKTICK",
			metadata: "[config]\nquote-character = BACKTICK\n",
			writeIt:  true,
			want:     false,
		},
		{
			name:     "no quote-character key",
			metadata: "#Started dump at: 2026-01-01 00:00:00\n",
			writeIt:  true,
			want:     false,
		},
		{
			name:    "no metadata file at all",
			writeIt: false,
			want:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.writeIt {
				if err := os.WriteFile(filepath.Join(dir, "metadata"), []byte(tc.metadata), 0644); err != nil {
					t.Fatalf("write metadata: %v", err)
				}
			}
			if got := dumpMetadataQuoteCharacterIsDoubleQuote(dir); got != tc.want {
				t.Errorf("dumpMetadataQuoteCharacterIsDoubleQuote() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestStripRemovedSQLModeValues guards a real bug: mydumper copies the
// source server's @@SQL_MODE verbatim into a preamble line written to
// every file it produces. NO_AUTO_CREATE_USER is valid on MariaDB/older
// MySQL but was removed (now a hard error) in MySQL 8.0, so restoring a
// dump from such a source into a modern MySQL 8+ destination crashed
// myloader on the very first file it touched.
func TestStripRemovedSQLModeValues(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "value in the middle",
			input: `SET SQL_MODE='NO_AUTO_VALUE_ON_ZERO,NO_AUTO_CREATE_USER,NO_ENGINE_SUBSTITUTION';`,
			want:  `SET SQL_MODE='NO_AUTO_VALUE_ON_ZERO,NO_ENGINE_SUBSTITUTION';`,
		},
		{
			name:  "value first",
			input: `SET SQL_MODE='NO_AUTO_CREATE_USER,NO_ENGINE_SUBSTITUTION';`,
			want:  `SET SQL_MODE='NO_ENGINE_SUBSTITUTION';`,
		},
		{
			name:  "value last",
			input: `SET SQL_MODE='NO_AUTO_VALUE_ON_ZERO,NO_AUTO_CREATE_USER';`,
			want:  `SET SQL_MODE='NO_AUTO_VALUE_ON_ZERO';`,
		},
		{
			name:  "value alone",
			input: `SET SQL_MODE='NO_AUTO_CREATE_USER';`,
			want:  `SET SQL_MODE='';`,
		},
		{
			name:  "value absent, untouched",
			input: `SET SQL_MODE='NO_AUTO_VALUE_ON_ZERO,NO_ENGINE_SUBSTITUTION';`,
			want:  `SET SQL_MODE='NO_AUTO_VALUE_ON_ZERO,NO_ENGINE_SUBSTITUTION';`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := stripRemovedSQLModeValues([]byte(tc.input))
			if string(got) != tc.want {
				t.Errorf("got: %q\nwant: %q", got, tc.want)
			}
			wantChanged := tc.input != tc.want
			if changed != wantChanged {
				t.Errorf("changed = %v, want %v", changed, wantChanged)
			}
		})
	}
}

// TestPatchSQLModeFile_StreamsLargeDataFileUnchangedAfterHeader guards
// against a real risk in the streaming implementation: only the header
// should ever be buffered in memory, with the rest of a (potentially
// multi-gigabyte) data file streamed through unchanged. This uses a
// smaller size for test speed, but exercises the same streaming path —
// Peek the header, patch it, Discard, then io.Copy the remainder.
func TestPatchSQLModeFile_StreamsLargeDataFileUnchangedAfterHeader(t *testing.T) {
	dir := t.TempDir()

	header := "/*!40101 SET SQL_MODE='NO_AUTO_VALUE_ON_ZERO,NO_AUTO_CREATE_USER,NO_ENGINE_SUBSTITUTION'*/;\n"
	wantHeader := "/*!40101 SET SQL_MODE='NO_AUTO_VALUE_ON_ZERO,NO_ENGINE_SUBSTITUTION'*/;\n"

	// A body well past sqlModePreambleWindow, so the streaming path
	// (rather than a whole-file read) is actually what's under test.
	var bodyBuilder strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&bodyBuilder, "(%d,\"row %d\"),\n", i, i)
	}
	body := bodyBuilder.String()
	if len(header)+len(body) <= sqlModePreambleWindow {
		t.Fatalf("test body too small to exercise streaming: %d bytes, want > %d", len(header)+len(body), sqlModePreambleWindow)
	}

	path := filepath.Join(dir, "mydb.orders.00000.sql")
	if err := os.WriteFile(path, []byte(header+body), 0644); err != nil {
		t.Fatalf("write data file: %v", err)
	}

	if err := patchSQLModeFile(path); err != nil {
		t.Fatalf("patchSQLModeFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read patched file: %v", err)
	}
	want := wantHeader + body
	if string(got) != want {
		t.Errorf("patched content mismatch (len got=%d want=%d)", len(got), len(want))
	}
}

// TestPatchSQLModeFile_NoChangeLeavesFileUntouched guards against
// unnecessary rewrites: a file whose preamble has nothing to strip must
// not be touched at all (no temp file, no rename, mtime unchanged).
func TestPatchSQLModeFile_NoChangeLeavesFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mydb.orders-schema.sql")
	content := "/*!40101 SET SQL_MODE='NO_AUTO_VALUE_ON_ZERO'*/;\nCREATE TABLE `orders` (`id` int NOT NULL);\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat before patch: %v", err)
	}

	if err := patchSQLModeFile(path); err != nil {
		t.Fatalf("patchSQLModeFile: %v", err)
	}

	infoAfter, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after patch: %v", err)
	}
	if !infoAfter.ModTime().Equal(info.ModTime()) {
		t.Error("file was rewritten even though nothing needed to change")
	}
}
