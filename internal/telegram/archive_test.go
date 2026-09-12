package telegram

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestTarDir(t *testing.T) {
	dir := t.TempDir()

	files := map[string]string{
		"mydb.users-schema.sql.gz":  "schema-users",
		"mydb.orders-schema.sql.gz": "schema-orders",
		"metadata":                  "some metadata content",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	tarPath, size, err := tarDir(dir)
	if err != nil {
		t.Fatalf("tarDir: %v", err)
	}
	defer os.Remove(tarPath)

	info, err := os.Stat(tarPath)
	if err != nil {
		t.Fatalf("stat tar: %v", err)
	}
	if info.Size() != size {
		t.Errorf("returned size %d does not match actual file size %d", size, info.Size())
	}

	f, err := os.Open(tarPath)
	if err != nil {
		t.Fatalf("open tar: %v", err)
	}
	defer f.Close()

	got := map[string]string{}
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar entry %s: %v", hdr.Name, err)
		}
		got[hdr.Name] = string(data)
	}

	if len(got) != len(files) {
		t.Fatalf("tar contains %d entries, want %d: %v", len(got), len(files), got)
	}
	for name, want := range files {
		if got[name] != want {
			t.Errorf("entry %q content = %q, want %q", name, got[name], want)
		}
	}
}
