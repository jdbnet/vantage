package httpapi

import (
	"os"
	"path/filepath"
	"testing"
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
