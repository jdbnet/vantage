package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jdbnet/vantage/internal/model"
	"github.com/jdbnet/vantage/internal/store"
)

type stubDirEntry struct {
	dir bool
}

func (d stubDirEntry) Name() string               { return "Download" }
func (d stubDirEntry) IsDir() bool                { return d.dir }
func (d stubDirEntry) Type() os.FileMode          { return os.ModeDir }
func (d stubDirEntry) Info() (os.FileInfo, error) { return nil, os.ErrPermission }

func TestJailJoin(t *testing.T) {
	root := t.TempDir()
	inside, err := jailJoin(root, "/ok.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "ok.txt")
	if inside != want {
		t.Fatalf("got %s want %s", inside, want)
	}
	cleaned, err := jailJoin(root, "/../outside.txt")
	if err != nil {
		t.Fatal(err)
	}
	if cleaned != filepath.Join(root, "outside.txt") {
		t.Fatalf("dot-dot should stay in jail, got %s", cleaned)
	}
	nested := filepath.Join(root, "a")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := jailJoin(root, "/a/../../outside")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(root, "outside") {
		t.Fatalf("nested dot-dot should stay in jail, got %s", got)
	}
}

func TestGuacdDrivePath(t *testing.T) {
	s := &Server{d: Deps{DataDir: "/data"}}
	got := s.guacdDrivePath(model.Settings{})
	if got != "/data/shared" && !strings.HasSuffix(got, string(os.PathSeparator)+"shared") {
		t.Fatalf("default guacd path %s", got)
	}
	if p := s.guacdDrivePath(model.Settings{GuacdDrivePath: "/drive"}); p != "/drive" {
		t.Fatalf("override %s", p)
	}
}

func TestSharedFilesDirDesktopIgnoresSetting(t *testing.T) {
	dir := t.TempDir()
	s := &Server{d: Deps{DataDir: dir, Mode: "desktop"}}
	got := s.sharedFilesDir(model.Settings{SharedFilesDir: "/data/shared"})
	want := filepath.Join(dir, "shared")
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestGuacdIsLoopback(t *testing.T) {
	if !guacdIsLoopback("127.0.0.1:4822") || !guacdIsLoopback("localhost") || !guacdIsLoopback("[::1]:4822") {
		t.Fatal("expected loopback")
	}
	if guacdIsLoopback("10.10.60.2:4822") || guacdIsLoopback("guacd:4822") {
		t.Fatal("expected remote")
	}
}

func TestRDPDrivePathUsesVantagedWhenRemoteGuacd(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sync/drive" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"drive_path": "/data/shared"})
	}))
	t.Cleanup(remote.Close)

	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "vantage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.SaveSettings(map[string]string{
		"sync_url":     remote.URL,
		"sync_api_key": "k",
		"guacd_addr":   "10.10.60.2:4822",
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{d: Deps{Store: st, DataDir: dir, Mode: "desktop"}}
	loaded, err := st.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got := s.rdpDrivePath(loaded); got != "/data/shared" {
		t.Fatalf("got %s", got)
	}
}

func TestRDPDrivePathLocalGuacdStaysLocal(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "vantage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.SaveSettings(map[string]string{
		"sync_url":     "http://vantaged.example",
		"sync_api_key": "k",
		"guacd_addr":   "127.0.0.1:4822",
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{d: Deps{Store: st, DataDir: dir, Mode: "desktop"}}
	loaded, err := st.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	got := s.rdpDrivePath(loaded)
	want, _ := filepath.Abs(filepath.Join(dir, "shared"))
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestSkipWalkErr(t *testing.T) {
	root := "/data/shared"
	err := skipWalkErr(root, filepath.Join(root, "Download"), stubDirEntry{dir: true}, os.ErrPermission)
	if err != filepath.SkipDir {
		t.Fatalf("unreadable dir: got %v want SkipDir", err)
	}
	err = skipWalkErr(root, filepath.Join(root, "secret.txt"), stubDirEntry{dir: false}, os.ErrPermission)
	if err != nil {
		t.Fatalf("unreadable file: got %v want nil", err)
	}
	err = skipWalkErr(root, root, stubDirEntry{dir: true}, os.ErrPermission)
	if err != os.ErrPermission {
		t.Fatalf("unreadable root: got %v want permission", err)
	}
}

func TestListSharedTreeRepairsOwnedDir(t *testing.T) {
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

	files, err := listSharedTree(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f.Path] = f.Dir
	}
	if _, ok := got["ok.txt"]; !ok {
		t.Fatalf("missing ok.txt: %+v", files)
	}
	if _, ok := got["locked/secret.txt"]; !ok {
		t.Fatalf("owned 000 dir should be chmod'd and listed: %+v", files)
	}
}

func TestIgnoredSharedPath(t *testing.T) {
	if !ignoredSharedPath("Download") || !ignoredSharedPath("/Download/file.txt") {
		t.Fatal("Download should be ignored")
	}
	if ignoredSharedPath("docs") || ignoredSharedPath("docs/Download") || ignoredSharedPath("Downloads") {
		t.Fatal("other paths should sync")
	}
}

func TestListSharedTreeSkipsDownload(t *testing.T) {
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
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}

	files, err := listSharedTree(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f.Path] = true
	}
	if !got["ok.txt"] || !got["docs"] {
		t.Fatalf("missing expected paths: %+v", files)
	}
	if got["Download"] || got["Download/secret.txt"] {
		t.Fatalf("Download should be excluded from sync: %+v", files)
	}
}
