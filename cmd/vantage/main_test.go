package main

import (
	"path/filepath"
	"testing"
)

func TestDesktopDataDirEnv(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom")
	t.Setenv("VANTAGE_DATA_DIR", want)
	if got := desktopDataDir(); got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}
