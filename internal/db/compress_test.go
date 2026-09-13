package db

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectCompression(t *testing.T) {
	tests := []struct {
		path string
		want compressionFormat
	}{
		{"mydb.orders-schema.sql.gz", compressionGzip},
		{"mydb.orders-schema.sql.zst", compressionZstd},
		{"mydb.orders-schema.sql", compressionNone},
		{"metadata", compressionNone},
	}
	for _, tc := range tests {
		if got := detectCompression(tc.path); got != tc.want {
			t.Errorf("detectCompression(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestWriteCompressed_OpenCompressed_RoundTrip(t *testing.T) {
	content := []byte("CREATE TABLE `orders` (`id` int NOT NULL);")

	for _, tc := range []struct {
		name   string
		format compressionFormat
		ext    string
	}{
		{"gzip", compressionGzip, ".gz"},
		{"zstd", compressionZstd, ".zst"},
		{"none", compressionNone, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "schema.sql"+tc.ext)

			var buf bytes.Buffer
			if err := writeCompressed(&buf, tc.format, content); err != nil {
				t.Fatalf("writeCompressed: %v", err)
			}
			if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
				t.Fatalf("write file: %v", err)
			}

			r, err := openCompressed(path)
			if err != nil {
				t.Fatalf("openCompressed: %v", err)
			}
			defer r.Close()

			got, err := io.ReadAll(r)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if !bytes.Equal(got, content) {
				t.Errorf("got %q, want %q", got, content)
			}
		})
	}
}
