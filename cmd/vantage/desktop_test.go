package main

import (
	"testing"
)

func TestDesktopDownloadURL(t *testing.T) {
	if _, err := desktopDownloadURL("/api/sftp/abc/download?path=a.csv&token=t"); err != nil {
		t.Fatal(err)
	}
	if _, err := desktopDownloadURL("http://127.0.0.1:7688/api/sftp/abc/download"); err == nil {
		t.Fatal("expected absolute urls to be rejected")
	}
	if _, err := desktopDownloadURL("/etc/passwd"); err == nil {
		t.Fatal("expected non-api paths to be rejected")
	}
}

func TestDesktopSaveFileWithoutContext(t *testing.T) {
	d := NewDesktop()
	path, err := d.SaveFile("test.txt", []int{'h', 'i'})
	if err == nil {
		t.Fatal("expected error when desktop context is not set")
	}
	if path != "" {
		t.Fatalf("path = %q", path)
	}
}
