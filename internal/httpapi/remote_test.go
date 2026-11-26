package httpapi

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jdbnet/vantage/internal/explorer"
	"github.com/jdbnet/vantage/internal/store"
)

type memFS struct {
	mu    sync.Mutex
	files map[string][]byte
	dirs  map[string]bool
	at    bool
}

func newMem(at bool) *memFS {
	return &memFS{files: map[string][]byte{}, dirs: map[string]bool{"/": true}, at: at}
}

func (m *memFS) Connect() error    { return nil }
func (m *memFS) Disconnect() error { return nil }

func memPath(p string) string {
	if p == "" || p == "." {
		return "/"
	}
	if !path.IsAbs(p) {
		p = "/" + p
	}
	return path.Clean(p)
}

func (m *memFS) Stat(p string) (explorer.FileEntry, error) {
	p = memPath(p)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dirs[p] {
		return explorer.FileEntry{Name: path.Base(p), Path: p, IsDir: true}, nil
	}
	b, ok := m.files[p]
	if !ok {
		return explorer.FileEntry{}, os.ErrNotExist
	}
	return explorer.FileEntry{Name: path.Base(p), Path: p, Size: int64(len(b))}, nil
}

func (m *memFS) ListDir(p string) ([]explorer.FileEntry, error) {
	p = memPath(p)
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []explorer.FileEntry
	for dir := range m.dirs {
		if path.Dir(dir) == p && dir != p {
			out = append(out, explorer.FileEntry{Name: path.Base(dir), Path: dir, IsDir: true})
		}
	}
	for name, b := range m.files {
		if path.Dir(name) == p {
			out = append(out, explorer.FileEntry{Name: path.Base(name), Path: name, Size: int64(len(b))})
		}
	}
	return out, nil
}

func (m *memFS) MkDir(p string) error {
	p = memPath(p)
	m.mu.Lock()
	defer m.mu.Unlock()
	for p != "/" {
		m.dirs[p] = true
		p = path.Dir(p)
	}
	m.dirs["/"] = true
	return nil
}

func (m *memFS) Delete(p string) error {
	p = memPath(p)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, p)
	delete(m.dirs, p)
	return nil
}

func (m *memFS) Rename(src, dst string) error {
	src, dst = memPath(src), memPath(dst)
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.files[src]; ok {
		m.files[dst] = b
		delete(m.files, src)
		return nil
	}
	return os.ErrNotExist
}

func (m *memFS) ReadFile(p string) (io.ReadCloser, error) {
	p = memPath(p)
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	cp := append([]byte(nil), b...)
	if m.at {
		return readerAt{Reader: bytes.NewReader(cp)}, nil
	}
	return io.NopCloser(bytes.NewReader(cp)), nil
}

func (m *memFS) WriteFile(p string, r io.Reader, _ int64) error {
	p = memPath(p)
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[p] = b
	dir := path.Dir(p)
	for dir != "/" {
		m.dirs[dir] = true
		dir = path.Dir(dir)
	}
	m.dirs["/"] = true
	return nil
}

func (m *memFS) Checksum(string) (string, error) { return "", nil }

type readerAt struct{ *bytes.Reader }

func (readerAt) Close() error { return nil }

type rangeFS struct{ data []byte }

func (rangeFS) Connect() error    { return nil }
func (rangeFS) Disconnect() error { return nil }
func (f rangeFS) Stat(string) (explorer.FileEntry, error) {
	return explorer.FileEntry{Name: "clip.mp4", Path: "/clip.mp4", Size: int64(len(f.data))}, nil
}
func (rangeFS) ListDir(string) ([]explorer.FileEntry, error) { return nil, nil }
func (rangeFS) MkDir(string) error                           { return nil }
func (rangeFS) Delete(string) error                          { return nil }
func (rangeFS) Rename(string, string) error                  { return nil }
func (f rangeFS) ReadFile(string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.data)), nil
}
func (rangeFS) WriteFile(string, io.Reader, int64) error { return nil }
func (rangeFS) Checksum(string) (string, error)          { return "", nil }
func (f rangeFS) ReadRange(_ string, start, length int64) (io.ReadCloser, error) {
	end := start + length
	if length == 0 {
		end = int64(len(f.data))
	}
	return io.NopCloser(bytes.NewReader(f.data[start:end])), nil
}

func TestRemoteStreamRangeAndFallback(t *testing.T) {
	body := []byte("abcdefghijklmnopqrstuvwxyz")
	req := httptest.NewRequest(http.MethodGet, "/stream?path=/clip.mp4", nil)
	req.Header.Set("Range", "bytes=0-3")
	rec := httptest.NewRecorder()
	serveExplorer(rec, req, "clip.mp4", rangeFS{data: body}, "/clip.mp4")
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "abcd" {
		t.Fatalf("range status %d body %q range %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Range"))
	}

	plain := newMem(false)
	plain.files["/note.txt"] = body
	req = httptest.NewRequest(http.MethodGet, "/stream?path=/note.txt", nil)
	req.Header.Set("Range", "bytes=0-3")
	rec = httptest.NewRecorder()
	serveExplorer(rec, req, "note.txt", plain, "/note.txt")
	if rec.Code != http.StatusOK || rec.Body.String() != string(body) {
		t.Fatalf("fallback status %d body %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Range") != "" {
		t.Fatalf("fallback advertised a range: %s", rec.Header().Get("Content-Range"))
	}

	seeker := newMem(true)
	seeker.files["/note.txt"] = body
	req = httptest.NewRequest(http.MethodGet, "/stream?path=/note.txt", nil)
	req.Header.Set("Range", "bytes=0-3")
	rec = httptest.NewRecorder()
	serveExplorer(rec, req, "note.txt", seeker, "/note.txt")
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "abcd" {
		t.Fatalf("readerat status %d body %q", rec.Code, rec.Body.String())
	}
}

func TestTransferLocalRemoteAndSFTP(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	remote := newMem(false)
	live := newMem(false)
	s := &Server{transfers: newTransferBook()}
	s.openHook = func(e fileEnd, _ bool) (explorer.Explorer, func(), error) {
		switch e.System {
		case "local":
			return explorer.NewLocal(), func() {}, nil
		case "sftp":
			return live, func() {}, nil
		default:
			return remote, func() {}, nil
		}
	}
	job, err := s.transfers.start(s, []fileEnd{{System: "local", Path: src}}, fileEnd{System: "remote", ConnID: "r1", Path: "/inbox"})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, "done")
	if string(remote.files["/inbox/a.txt"]) != "hello" {
		t.Fatalf("remote file %q", remote.files["/inbox/a.txt"])
	}

	job, err = s.transfers.start(s, []fileEnd{{System: "remote", ConnID: "r1", Path: "/inbox/a.txt"}}, fileEnd{System: "local", Path: dst})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, "done")
	got, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("local %q %v", got, err)
	}

	job, err = s.transfers.start(s, []fileEnd{{System: "remote", ConnID: "r1", Path: "/inbox/a.txt"}}, fileEnd{System: "sftp", ConnID: "sess", Path: "/home"})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, "done")
	if string(live.files["/home/a.txt"]) != "hello" {
		t.Fatalf("sftp %q", live.files["/home/a.txt"])
	}
}

func TestExclusiveRemoteDoesNotOverlap(t *testing.T) {
	gate := &gateFS{}
	p := &remotePool{
		items: map[string]*remoteConn{},
		stop:  make(chan struct{}),
		load: func(string) (explorer.Config, error) {
			return explorer.Config{Protocol: "ftp"}, nil
		},
		dial: func(explorer.Config) (explorer.Explorer, error) {
			return gate, nil
		},
	}
	defer close(p.stop)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			exp, release, err := p.acquire("same", false)
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = exp.ListDir("/")
			release()
		}()
	}
	wg.Wait()
	if gate.overlap {
		t.Fatal("cached ftp connection ran two operations at once")
	}
}

type gateFS struct {
	mu      sync.Mutex
	active  int
	overlap bool
}

func (g *gateFS) Connect() error    { return nil }
func (g *gateFS) Disconnect() error { return nil }
func (g *gateFS) ListDir(string) ([]explorer.FileEntry, error) {
	g.mu.Lock()
	g.active++
	if g.active > 1 {
		g.overlap = true
	}
	g.mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	g.mu.Lock()
	g.active--
	g.mu.Unlock()
	return nil, nil
}
func (g *gateFS) Stat(string) (explorer.FileEntry, error) {
	return explorer.FileEntry{Path: "/", IsDir: true}, nil
}
func (g *gateFS) MkDir(string) error          { return nil }
func (g *gateFS) Delete(string) error         { return nil }
func (g *gateFS) Rename(string, string) error { return nil }
func (g *gateFS) ReadFile(string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (g *gateFS) WriteFile(string, io.Reader, int64) error { return nil }
func (g *gateFS) Checksum(string) (string, error)          { return "", nil }

func TestDedicatedFTPOpensItsOwnConnection(t *testing.T) {
	var n int
	p := &remotePool{
		items: map[string]*remoteConn{},
		stop:  make(chan struct{}),
		load: func(string) (explorer.Config, error) {
			return explorer.Config{Protocol: "ftp"}, nil
		},
		dial: func(explorer.Config) (explorer.Explorer, error) {
			n++
			return newMem(false), nil
		},
	}
	defer close(p.stop)
	_, releaseA, err := p.acquire("same", true)
	if err != nil {
		t.Fatal(err)
	}
	_, releaseB, err := p.acquire("same", true)
	if err != nil {
		t.Fatal(err)
	}
	releaseA()
	releaseB()
	if n != 2 {
		t.Fatalf("dedicated ftp dials = %d, want 2", n)
	}
}

func TestHTTPRemoteListOmitsSecret(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "vantage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateRemote(store.RemoteWrite{Label: "box", Protocol: "s3", Blob: "do-not-leak"}); err != nil {
		t.Fatal(err)
	}
	s := &Server{d: Deps{Store: st}}
	rec := httptest.NewRecorder()
	s.handleListRemotes(rec, httptest.NewRequest(http.MethodGet, "/api/remotes", nil))
	if rec.Code != 200 || bytes.Contains(rec.Body.Bytes(), []byte("do-not-leak")) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
}
