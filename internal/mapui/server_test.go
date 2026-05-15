package mapui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerMeta(t *testing.T) {
	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/meta", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var meta metaResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Input == "" || meta.ConfigOut == "" || len(meta.Palette) != len(Palette) {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}

func TestServerConfigWritesYAML(t *testing.T) {
	server := newTestServer(t)
	body := ConfigRequest{
		OutDir:        "out/generated",
		TargetBitrate: "500k",
		Encoder:       "auto",
		BitrateWindow: 2,
		ROIBlockSize:  64,
		Cells: []PaintCell{
			{Col: 1, Row: 1, QOffset: -0.40},
			{Col: 2, Row: 1, QOffset: -0.40},
		},
	}
	rec := postJSON(t, server, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response configResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.BlockCount != 2 || response.RectCount != 1 {
		t.Fatalf("unexpected response: %+v", response)
	}

	data, err := os.ReadFile(server.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "mode: blocks") || !strings.Contains(string(data), "qoffset: -0.4") {
		t.Fatalf("unexpected YAML:\n%s", string(data))
	}
}

func TestServerConfigRejectsInvalidBlocksAndEmptyMap(t *testing.T) {
	server := newTestServer(t)

	empty := ConfigRequest{
		OutDir:        "out/generated",
		TargetBitrate: "500k",
		Encoder:       "auto",
		BitrateWindow: 2,
		ROIBlockSize:  64,
	}
	rec := postJSON(t, server, empty)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty status = %d, want 400", rec.Code)
	}

	invalid := empty
	invalid.Cells = []PaintCell{{Col: 1, Row: 1, QOffset: -0.33}}
	rec = postJSON(t, server, invalid)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d, want 400", rec.Code)
	}
}

func TestServerVideoSupportsGet(t *testing.T) {
	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/video", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "video" {
		t.Fatalf("video body = %q", rec.Body.String())
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()

	dir := t.TempDir()
	video := filepath.Join(dir, "video.mp4")
	writeTestFile(t, video)

	opts := DefaultOptions()
	opts.Input = video
	opts.ConfigOut = filepath.Join(dir, "generated.yaml")
	opts.OpenBrowser = false

	server, err := NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func postJSON(t *testing.T, server *Server, value any) *httptest.ResponseRecorder {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	return rec
}
