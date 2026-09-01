package syncx

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/jdbnet/vantage/internal/store"
)

func ageFile(t *testing.T, path string) {
	t.Helper()
	past := time.Now().Add(-10 * time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
}

func TestWalkShared(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("there"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := walkShared(dir)
	if err != nil {
		t.Fatal(err)
	}
	byPath := indexFiles(files)
	if _, ok := byPath["a.txt"]; !ok {
		t.Fatalf("missing a.txt: %+v", files)
	}
	if _, ok := byPath["sub"]; !ok || !byPath["sub"].Dir {
		t.Fatalf("missing sub dir: %+v", files)
	}
	if byPath["sub/b.txt"].Size != 5 {
		t.Fatalf("b.txt size %d", byPath["sub/b.txt"].Size)
	}
}

func TestWalkSharedSkipsUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(root, "locked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "secret.txt"), []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })

	files, err := walkShared(root)
	if err != nil {
		t.Fatal(err)
	}
	byPath := indexFiles(files)
	if _, ok := byPath["ok.txt"]; !ok {
		t.Fatalf("missing ok.txt: %+v", files)
	}
	if _, ok := byPath["locked/secret.txt"]; ok {
		t.Fatalf("should skip unreadable dir contents: %+v", files)
	}
}

func TestWalkSharedSkipsDownload(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "Download"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Download", "secret.txt"), []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := walkShared(root)
	if err != nil {
		t.Fatal(err)
	}
	byPath := indexFiles(files)
	if _, ok := byPath["ok.txt"]; !ok {
		t.Fatalf("missing ok.txt: %+v", files)
	}
	if _, ok := byPath["Download"]; ok {
		t.Fatalf("Download should be excluded from sync: %+v", files)
	}
	if _, ok := byPath["Download/secret.txt"]; ok {
		t.Fatalf("Download contents should be excluded: %+v", files)
	}
}

func TestReconcileSharedFilesUploadDownloadDelete(t *testing.T) {
	remote := t.TempDir()
	srv := httptest.NewServer(sharedFileStub(remote))
	t.Cleanup(srv.Close)

	st, err := store.Open(filepath.Join(t.TempDir(), "client.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	ageFile(t, filepath.Join(local, "hello.txt"))
	if err := reconcileSharedFiles(st, local, srv.URL, "k"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(remote, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("remote hello.txt = %q", got)
	}

	if err := os.WriteFile(filepath.Join(remote, "from-server.txt"), []byte("srv"), 0o644); err != nil {
		t.Fatal(err)
	}
	ageFile(t, filepath.Join(remote, "from-server.txt"))
	if err := reconcileSharedFiles(st, local, srv.URL, "k"); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(local, "from-server.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "srv" {
		t.Fatalf("local from-server.txt = %q", got)
	}

	if err := os.Remove(filepath.Join(local, "hello.txt")); err != nil {
		t.Fatal(err)
	}
	if err := reconcileSharedFiles(st, local, srv.URL, "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(remote, "hello.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected remote hello.txt deleted, err=%v", err)
	}
}

func TestReconcileSkipsRecentlyWrittenFile(t *testing.T) {
	remote := t.TempDir()
	srv := httptest.NewServer(sharedFileStub(remote))
	t.Cleanup(srv.Close)

	st, err := store.Open(filepath.Join(t.TempDir(), "client.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	local := t.TempDir()
	path := filepath.Join(local, "copying.txt")
	if err := os.WriteFile(path, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := reconcileSharedFiles(st, local, srv.URL, "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(remote, "copying.txt")); !os.IsNotExist(err) {
		t.Fatal("expected recently written file to wait before upload")
	}

	ageFile(t, path)
	if err := reconcileSharedFiles(st, local, srv.URL, "k"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(remote, "copying.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "partial" {
		t.Fatalf("remote copying.txt = %q", got)
	}
}

func TestReconcileDoesNotReplaceRemoteWithUnstableLocal(t *testing.T) {
	remote := t.TempDir()
	srv := httptest.NewServer(sharedFileStub(remote))
	t.Cleanup(srv.Close)

	st, err := store.Open(filepath.Join(t.TempDir(), "client.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "doc.bin"), []byte("complete"), 0o644); err != nil {
		t.Fatal(err)
	}
	ageFile(t, filepath.Join(local, "doc.bin"))
	if err := reconcileSharedFiles(st, local, srv.URL, "k"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(local, "doc.bin"), []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := reconcileSharedFiles(st, local, srv.URL, "k"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(remote, "doc.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "complete" {
		t.Fatalf("remote should keep last complete copy, got %q", got)
	}
}

func sharedFileStub(root string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sync/files", func(w http.ResponseWriter, r *http.Request) {
		files, err := walkShared(root)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"files": files})
	})
	mux.HandleFunc("GET /api/sync/files/content", func(w http.ResponseWriter, r *http.Request) {
		abs := filepath.Join(root, filepath.FromSlash(r.URL.Query().Get("path")))
		f, err := os.Open(abs)
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("X-Vantage-Mtime", strconv.FormatInt(st.ModTime().Unix(), 10))
		_, _ = io.Copy(w, f)
	})
	mux.HandleFunc("PUT /api/sync/files/content", func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("path")
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if r.Header.Get("X-Vantage-Dir") == "1" {
			_ = os.MkdirAll(abs, 0o777)
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o777); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		dst, err := os.Create(abs)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		_, _ = io.Copy(dst, r.Body)
		_ = dst.Close()
		if ms := r.Header.Get("X-Vantage-Mtime"); ms != "" {
			if unix, err := strconv.ParseInt(ms, 10, 64); err == nil && unix > 0 {
				t := time.Unix(unix, 0)
				_ = os.Chtimes(abs, t, t)
			}
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("DELETE /api/sync/files/content", func(w http.ResponseWriter, r *http.Request) {
		abs := filepath.Join(root, filepath.FromSlash(r.URL.Query().Get("path")))
		_ = os.RemoveAll(abs)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	return mux
}
