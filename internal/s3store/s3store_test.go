package s3store

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"dbtool/internal/backupstate"
	"dbtool/internal/settings"
)

func TestSafeObjectPath(t *testing.T) {
	for _, path := range []string{"../../escaped.sql", "x/../escaped.sql", "/absolute.sql", ".", "", ".."} {
		if _, err := safeObjectPath(path); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	if path, err := safeObjectPath("nested/table.sql"); err != nil || path != filepath.Join("nested", "table.sql") {
		t.Fatalf("%s %v", path, err)
	}
}

func testS3(t *testing.T, handler http.HandlerFunc) settings.S3Config {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return settings.S3Config{Bucket: "bucket", Region: "us-east-1", AccessKey: "test", SecretKey: "test", Endpoint: server.URL}
}

func TestDownloadRejectsTraversalAndSymlinks(t *testing.T) {
	for _, key := range []string{"../../escaped.sql", "link/escaped.sql"} {
		t.Run(key, func(t *testing.T) {
			cfg := testS3(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "HEAD" {
					if strings.HasSuffix(r.URL.Path, backupstate.Complete) {
						w.WriteHeader(200)
					} else {
						w.WriteHeader(404)
					}
					return
				}
				if r.URL.Query().Get("list-type") == "2" {
					w.Header().Set("Content-Type", "application/xml")
					fmt.Fprintf(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>%s</Key><Size>4</Size></Contents></ListBucketResult>`, html.EscapeString("dump/"+key))
					return
				}
				fmt.Fprint(w, "data")
			})
			dest := t.TempDir()
			os.Mkdir(filepath.Join(dest, "dump"), 0700)
			outside := t.TempDir()
			if err := os.Symlink(outside, filepath.Join(dest, "dump", "link")); err != nil {
				t.Fatal(err)
			}
			if _, err := DownloadDump(cfg, "dump", dest); err == nil {
				t.Fatal("unsafe download succeeded")
			}
			if _, err := os.Stat(filepath.Join(outside, "escaped.sql")); !os.IsNotExist(err) {
				t.Fatal("wrote outside root")
			}
		})
	}
}

func TestListDumpsSkipsPendingAndSupportsLegacy(t *testing.T) {
	cfg := testS3(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			exists := r.URL.Path == "/bucket/new/"+backupstate.Complete || r.URL.Path == "/bucket/legacy/metadata" || r.URL.Path == "/bucket/pending/"+backupstate.Pending || r.URL.Path == "/bucket/pending/"+backupstate.Complete
			if exists {
				w.WriteHeader(200)
			} else {
				w.WriteHeader(404)
			}
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated><CommonPrefixes><Prefix>new/</Prefix></CommonPrefixes><CommonPrefixes><Prefix>legacy/</Prefix></CommonPrefixes><CommonPrefixes><Prefix>pending/</Prefix></CommonPrefixes><CommonPrefixes><Prefix>partial/</Prefix></CommonPrefixes></ListBucketResult>`)
	})
	dumps, err := ListDumps(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(dumps, ",") != "new,legacy" {
		t.Fatalf("dumps: %v", dumps)
	}
}

func TestUploadPublishesCompletionLast(t *testing.T) {
	var mu sync.Mutex
	var operations []string
	cfg := testS3(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		operations = append(operations, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.WriteHeader(200)
	})
	dir := filepath.Join(t.TempDir(), "dump")
	os.Mkdir(dir, 0700)
	os.WriteFile(filepath.Join(dir, "metadata"), []byte("complete"), 0600)
	if err := UploadDir(cfg, dir); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"PUT /bucket/dump/" + backupstate.Pending, "PUT /bucket/dump/metadata", "PUT /bucket/dump/" + backupstate.Complete, "DELETE /bucket/dump/" + backupstate.Pending}
	if strings.Join(operations, ",") != strings.Join(want, ",") {
		t.Fatalf("operations: %v", operations)
	}
}

func TestFailedUploadStaysPending(t *testing.T) {
	var complete bool
	cfg := testS3(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "metadata") {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(403)
			fmt.Fprint(w, "<Error><Code>AccessDenied</Code></Error>")
			return
		}
		if strings.HasSuffix(r.URL.Path, backupstate.Complete) {
			complete = true
		}
		w.WriteHeader(200)
	})
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "metadata"), nil, 0600)
	if err := UploadDir(cfg, dir); err == nil {
		t.Fatal("upload error ignored")
	}
	if complete {
		t.Fatal("failed upload published completion")
	}
}

func TestConnectionWritesReadsAndDeletesProbe(t *testing.T) {
	var operations []string
	cfg := testS3(t, func(w http.ResponseWriter, r *http.Request) {
		operations = append(operations, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodGet:
			fmt.Fprint(w, "dbtool s3 connection test\n")
		default:
			w.WriteHeader(http.StatusOK)
		}
	})
	cfg.Prefix = "backups"
	if err := TestConnection(cfg); err != nil {
		t.Fatal(err)
	}
	if len(operations) != 3 {
		t.Fatalf("operations: %v", operations)
	}
	if !strings.HasPrefix(operations[0], "PUT /bucket/backups/.dbtool-test-") ||
		!strings.HasPrefix(operations[1], "GET /bucket/backups/.dbtool-test-") ||
		!strings.HasPrefix(operations[2], "DELETE /bucket/backups/.dbtool-test-") {
		t.Fatalf("operations: %v", operations)
	}
}
