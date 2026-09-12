package telegram

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withTestServer points apiBase at an httptest server for the duration of
// the test and restores it afterward.
func withTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	orig := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = orig })

	return srv
}

func okResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
}

func staticReader(data string) func() (io.Reader, error) {
	return func() (io.Reader, error) { return strings.NewReader(data), nil }
}

func TestSendMessageHTML_SetsParseMode(t *testing.T) {
	var gotParseMode, gotText string
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotParseMode = r.FormValue("parse_mode")
		gotText = r.FormValue("text")
		okResponse(w)
	})

	if err := sendMessageHTML("tok", "chat1", "<b>hi</b>"); err != nil {
		t.Fatalf("sendMessageHTML: %v", err)
	}
	if gotParseMode != "HTML" {
		t.Errorf("parse_mode = %q, want HTML", gotParseMode)
	}
	if gotText != "<b>hi</b>" {
		t.Errorf("text = %q, want <b>hi</b>", gotText)
	}
}

func TestSendMessage_NoParseMode(t *testing.T) {
	var sawParseMode bool
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Has("parse_mode") {
			sawParseMode = true
		}
		okResponse(w)
	})

	if err := sendMessage("tok", "chat1", "plain text"); err != nil {
		t.Fatalf("sendMessage: %v", err)
	}
	if sawParseMode {
		t.Error("plain sendMessage should not set parse_mode")
	}
}

func TestSendDocument_UploadsSingleFile(t *testing.T) {
	var gotFilename, gotCaption, gotContent string
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("parse content type: %v", err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("next part: %v", err)
			}
			data, _ := io.ReadAll(part)
			switch part.FormName() {
			case "caption":
				gotCaption = string(data)
			case "document":
				gotFilename = part.FileName()
				gotContent = string(data)
			}
		}
		okResponse(w)
	})

	doc := documentUpload{
		filename:  "dump.tar.part001",
		caption:   "dump (part 1/1)",
		newReader: staticReader("chunk-bytes"),
	}
	if err := sendDocument("tok", "chat1", doc); err != nil {
		t.Fatalf("sendDocument: %v", err)
	}
	if gotFilename != "dump.tar.part001" {
		t.Errorf("filename = %q, want dump.tar.part001", gotFilename)
	}
	if gotCaption != "dump (part 1/1)" {
		t.Errorf("caption = %q, want %q", gotCaption, "dump (part 1/1)")
	}
	if gotContent != "chunk-bytes" {
		t.Errorf("content = %q, want chunk-bytes", gotContent)
	}
}

func TestSendMediaGroup_GroupsFilesInOneRequest(t *testing.T) {
	var requestCount int
	type mediaEntry struct {
		Type    string `json:"type"`
		Media   string `json:"media"`
		Caption string `json:"caption"`
	}
	var gotMedia []mediaEntry
	gotFiles := map[string]string{}

	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("parse content type: %v", err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("next part: %v", err)
			}
			data, _ := io.ReadAll(part)
			switch {
			case part.FormName() == "media":
				if err := json.Unmarshal(data, &gotMedia); err != nil {
					t.Fatalf("unmarshal media: %v", err)
				}
			case part.FileName() != "":
				gotFiles[part.FormName()] = string(data)
			}
		}
		okResponse(w)
	})

	docs := []documentUpload{
		{filename: "dump.tar.part001", caption: "dump (part 1/3)", newReader: staticReader("part1")},
		{filename: "dump.tar.part002", caption: "dump (part 2/3)", newReader: staticReader("part2")},
		{filename: "dump.tar.part003", caption: "dump (part 3/3)", newReader: staticReader("part3")},
	}
	if err := sendMediaGroup("tok", "chat1", docs); err != nil {
		t.Fatalf("sendMediaGroup: %v", err)
	}

	// The whole point of grouping: all 3 files travel in exactly one HTTP
	// request instead of 3 separate sendDocument calls/messages.
	if requestCount != 1 {
		t.Fatalf("requestCount = %d, want 1 (all parts grouped into a single request)", requestCount)
	}
	if len(gotMedia) != 3 {
		t.Fatalf("media entries = %d, want 3", len(gotMedia))
	}
	for i, m := range gotMedia {
		if m.Type != "document" {
			t.Errorf("media[%d].Type = %q, want document", i, m.Type)
		}
		if !strings.HasPrefix(m.Media, "attach://") {
			t.Errorf("media[%d].Media = %q, want attach:// prefix", i, m.Media)
		}
		field := strings.TrimPrefix(m.Media, "attach://")
		if gotFiles[field] == "" {
			t.Errorf("no uploaded file found for field %q referenced by media[%d]", field, i)
		}
	}
	if gotMedia[1].Caption != "dump (part 2/3)" {
		t.Errorf("media[1].Caption = %q, want %q", gotMedia[1].Caption, "dump (part 2/3)")
	}
}

func TestSendMediaGroup_RejectsInvalidCount(t *testing.T) {
	tests := []struct {
		name string
		n    int
	}{
		{"zero items", 0},
		{"one item", 1},
		{"eleven items", 11},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			docs := make([]documentUpload, tc.n)
			for i := range docs {
				docs[i] = documentUpload{filename: "f", newReader: staticReader("x")}
			}
			if err := sendMediaGroup("tok", "chat1", docs); err == nil {
				t.Errorf("expected an error for %d documents", tc.n)
			}
		})
	}
}

func TestAnnouncementHTML(t *testing.T) {
	got := announcementHTML("mydb_2026-01-01_000000", 100*1024*1024, 3)

	for _, want := range []string{
		"<b>dbtool backup</b>",
		"<code>mydb_2026-01-01_000000</code>",
		"<b>Size:</b> 100.00 MB",
		"<b>Parts:</b> 3",
		"<pre>cat mydb_2026-01-01_000000.tar.part* &gt; mydb_2026-01-01_000000.tar &amp;&amp; tar -xf mydb_2026-01-01_000000.tar</pre>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("announcement missing %q\ngot: %s", want, got)
		}
	}
}
