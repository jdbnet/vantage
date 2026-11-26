package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanLocalPath(t *testing.T) {
	if _, err := cleanLocalPath("bad\x00path"); err == nil {
		t.Fatal("expected NUL to be rejected")
	}
	got, err := cleanLocalPath("")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("empty path %q", got)
	}
	abs, err := cleanLocalPath(filepath.Join(t.TempDir(), "a", "..", "b"))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(abs) || filepath.Base(abs) != "b" {
		t.Fatalf("cleaned %q", abs)
	}
}

func TestLocalStreamRange(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "note.txt")
	body := []byte("abcdefghijklmnopqrstuvwxyz")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/local/stream?path="+p, nil)
	req.Header.Set("Range", "bytes=0-3")
	rec := httptest.NewRecorder()
	s.handleLocalStream(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "abcd" {
		t.Fatalf("body %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 0-3/26" {
		t.Fatalf("content-range %q", got)
	}
	if rec.Header().Get("Content-Type") == "" {
		t.Fatal("missing content type")
	}
}

func TestTransferRecursiveAndConflict(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "b.txt"), []byte("xyz"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dst, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "nested", "a.txt"), []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{transfers: newTransferBook()}
	job, err := s.transfers.start(s, []fileEnd{{
		System: "local",
		Path:   filepath.Join(src, "nested"),
	}}, fileEnd{System: "local", Path: dst})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, "waiting")
	if err := s.transfers.resolve(job.ID, "overwrite", true); err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, "done")
	snap := jobSnapshot(t, s, job.ID)
	if snap.Total != 8 || snap.Bytes != 8 {
		t.Fatalf("progress %d/%d", snap.Bytes, snap.Total)
	}
	got, err := os.ReadFile(filepath.Join(dst, "nested", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("overwrite %q", got)
	}
	old, err := os.ReadFile(filepath.Join(dst, "nested", "b.txt"))
	if string(old) != "xyz" {
		t.Fatalf("unrelated file %q %v", old, err)
	}
}

func TestTransferSkipAndRename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dstDir := filepath.Join(dir, "dst")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dstDir, "src.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{transfers: newTransferBook()}
	job, err := s.transfers.start(s, []fileEnd{{System: "local", Path: src}}, fileEnd{System: "local", Path: dstDir})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, "waiting")
	if err := s.transfers.resolve(job.ID, "skip", false); err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, "done")
	got, _ := os.ReadFile(filepath.Join(dstDir, "src.txt"))
	if string(got) != "old" {
		t.Fatalf("skip wrote %q", got)
	}

	job, err = s.transfers.start(s, []fileEnd{{System: "local", Path: src}}, fileEnd{System: "local", Path: dstDir})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, "waiting")
	if err := s.transfers.resolve(job.ID, "rename", false); err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, "done")
	renamed, err := os.ReadFile(filepath.Join(dstDir, "src (1).txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(renamed) != "new" {
		t.Fatalf("rename %q", renamed)
	}
}

func TestTransferCancelOutlivesPost(t *testing.T) {
	gate := make(chan struct{})
	transferGate = gate
	t.Cleanup(func() { transferGate = nil })

	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	dst := filepath.Join(dir, "out")
	if err := os.WriteFile(src, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Server{transfers: newTransferBook()}
	payload, _ := json.Marshal(map[string]any{
		"sources": []fileEnd{{System: "local", Path: src}},
		"dest":    fileEnd{System: "local", Path: dst},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/files/transfers", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	s.handleCreateTransfer(rec, req)
	if rec.Code != 200 {
		t.Fatalf("post %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("missing id")
	}
	waitJob(t, s, created.ID, "running")
	s.transfers.cancelConn("sess-1")
	if jobSnapshot(t, s, created.ID).State != "running" {
		t.Fatal("unrelated session cancelled the job")
	}
	s.transfers.mu.Lock()
	job := s.transfers.jobs[created.ID]
	s.transfers.mu.Unlock()
	job.mu.Lock()
	job.srcConns = []string{"sess-1"}
	job.mu.Unlock()
	s.cancelTransfers("sess-1")
	waitJob(t, s, created.ID, "cancelled")
}

func waitJob(t *testing.T, s *Server, id, state string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, j := range s.transfers.list() {
			if j.ID == id && j.State == state {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach %s", id, state)
}

func jobSnapshot(t *testing.T, s *Server, id string) transferJob {
	t.Helper()
	for _, j := range s.transfers.list() {
		if j.ID == id {
			return j
		}
	}
	t.Fatalf("missing job %s", id)
	return transferJob{}
}
