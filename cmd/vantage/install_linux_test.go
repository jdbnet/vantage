//go:build linux

package main

import "testing"

func TestSkipSelfInstall(t *testing.T) {
	if !skipSelfInstall("/usr/bin/vantage") {
		t.Fatal("system package should skip")
	}
	if !skipSelfInstall("/opt/vantage/vantage") {
		t.Fatal("opt install should skip")
	}
	if skipSelfInstall("/tmp/vantage-download") {
		t.Fatal("ad-hoc binary should self-install")
	}
}
