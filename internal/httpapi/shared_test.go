package httpapi

import (
	"os"
	"path/filepath"
	"testing"
)

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
