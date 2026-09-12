package db

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
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
			got := patchSQL(tc.input)
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
