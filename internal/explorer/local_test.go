package explorer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalExplorerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	fs := NewLocal()
	folder := filepath.Join(dir, "folder")
	if err := fs.MkDir(folder); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(folder, "note.txt")
	if err := fs.WriteFile(file, strings.NewReader("hello"), 5); err != nil {
		t.Fatal(err)
	}
	st, err := fs.Stat(file)
	if err != nil || st.IsDir || st.Size != 5 {
		t.Fatalf("stat %+v %v", st, err)
	}
	entries, err := fs.ListDir(folder)
	if err != nil || len(entries) != 1 || entries[0].Name != "note.txt" {
		t.Fatalf("list %+v %v", entries, err)
	}
	renamed := filepath.Join(folder, "renamed.txt")
	if err := fs.Rename(file, renamed); err != nil {
		t.Fatal(err)
	}
	if err := fs.Delete(renamed); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(renamed); !os.IsNotExist(err) {
		t.Fatalf("expected missing file, got %v", err)
	}
}
