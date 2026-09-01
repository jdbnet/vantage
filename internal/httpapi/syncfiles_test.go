package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jdbnet/vantage/internal/store"
)

func TestSyncFilePutGetListDelete(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "vantage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := &Server{d: Deps{Store: st, DataDir: dir}}

	put := httptest.NewRequest(http.MethodPut, "/api/sync/files/content?path=sub/hi.txt", bytes.NewReader([]byte("hello")))
	put.Header.Set("X-Vantage-Mtime", "1700000000")
	rec := httptest.NewRecorder()
	s.handleSyncFilePut(rec, put)
	if rec.Code != 200 {
		t.Fatalf("put status %d body %s", rec.Code, rec.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/api/sync/files/content?path=sub/hi.txt", nil)
	rec = httptest.NewRecorder()
	s.handleSyncFileGet(rec, get)
	if rec.Code != 200 {
		t.Fatalf("get status %d body %s", rec.Code, rec.Body.String())
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "hello" {
		t.Fatalf("get body %q", body)
	}
	if rec.Header().Get("X-Vantage-Mtime") != "1700000000" {
		t.Fatalf("mtime header %q", rec.Header().Get("X-Vantage-Mtime"))
	}

	list := httptest.NewRequest(http.MethodGet, "/api/sync/files", nil)
	rec = httptest.NewRecorder()
	s.handleSyncFileList(rec, list)
	if rec.Code != 200 {
		t.Fatalf("list status %d body %s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Files []sharedFileMeta `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	byPath := map[string]sharedFileMeta{}
	for _, f := range listed.Files {
		byPath[f.Path] = f
	}
	if !byPath["sub"].Dir {
		t.Fatalf("expected sub dir in list: %+v", listed.Files)
	}
	if byPath["sub/hi.txt"].Size != 5 {
		t.Fatalf("expected hi.txt in list: %+v", listed.Files)
	}

	onDisk, err := os.ReadFile(filepath.Join(dir, "shared", "sub", "hi.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != "hello" {
		t.Fatalf("disk %q", onDisk)
	}

	del := httptest.NewRequest(http.MethodDelete, "/api/sync/files/content?path=sub/hi.txt", nil)
	rec = httptest.NewRecorder()
	s.handleSyncFileDelete(rec, del)
	if rec.Code != 200 {
		t.Fatalf("delete status %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "shared", "sub", "hi.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected file gone, err=%v", err)
	}
}

func TestSyncFileExcludesDownload(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "vantage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := &Server{d: Deps{Store: st, DataDir: dir}}
	if err := os.MkdirAll(filepath.Join(dir, "shared", "Download"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shared", "Download", "secret.txt"), []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shared", "ok.txt"), []byte("yes"), 0o644); err != nil {
		t.Fatal(err)
	}

	list := httptest.NewRequest(http.MethodGet, "/api/sync/files", nil)
	rec := httptest.NewRecorder()
	s.handleSyncFileList(rec, list)
	if rec.Code != 200 {
		t.Fatalf("list status %d body %s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Files []sharedFileMeta `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	for _, f := range listed.Files {
		if ignoredSharedPath(f.Path) {
			t.Fatalf("Download leaked into sync list: %+v", listed.Files)
		}
	}

	put := httptest.NewRequest(http.MethodPut, "/api/sync/files/content?path=Download/x.txt", bytes.NewReader([]byte("x")))
	rec = httptest.NewRecorder()
	s.handleSyncFilePut(rec, put)
	if rec.Code != 400 {
		t.Fatalf("put Download status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSyncDrive(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "vantage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.SaveSettings(map[string]string{"guacd_drive_path": "/data/shared"}); err != nil {
		t.Fatal(err)
	}
	s := &Server{d: Deps{Store: st, DataDir: dir}}
	rec := httptest.NewRecorder()
	s.handleSyncDrive(rec, httptest.NewRequest(http.MethodGet, "/api/sync/drive", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		DrivePath string `json:"drive_path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.DrivePath != "/data/shared" {
		t.Fatalf("drive_path %q", body.DrivePath)
	}
}
