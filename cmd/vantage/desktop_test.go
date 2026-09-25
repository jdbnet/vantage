package main

import (
	"testing"
)

func TestDesktopSaveFileWithoutContext(t *testing.T) {
	d := NewDesktop()
	path, err := d.SaveFile("test.txt", []byte("hi"))
	if err == nil {
		t.Fatal("expected error when desktop context is not set")
	}
	if path != "" {
		t.Fatalf("path = %q", path)
	}
}
