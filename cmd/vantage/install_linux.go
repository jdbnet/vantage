//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jdbnet/vantage/packaging"
)

func maybeSelfInstall() {
	if os.Getenv("VANTAGE_NO_INSTALL") != "" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return
	}
	if skipSelfInstall(exe) {
		return
	}
	destDir, destBin, err := installDest()
	if err != nil {
		logInstall("install skipped: %v", err)
		return
	}
	if sameFile(exe, destBin) {
		return
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		logInstall("install skipped: %v", err)
		return
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		logInstall("install skipped: %v", err)
		return
	}
	tmp := destBin + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		logInstall("install skipped: %v", err)
		return
	}
	if err := os.Rename(tmp, destBin); err != nil {
		_ = os.Remove(tmp)
		logInstall("install skipped: %v", err)
		return
	}
	if err := writeDesktopFiles(destBin); err != nil {
		logInstall("menu entry: %v", err)
	}
	args := append([]string{destBin}, os.Args[1:]...)
	env := os.Environ()
	if err := syscall.Exec(destBin, args, env); err != nil {
		logInstall("failed to launch installed copy: %v", err)
		os.Exit(1)
	}
}

func skipSelfInstall(exe string) bool {
	if strings.HasPrefix(exe, "/usr/") || strings.HasPrefix(exe, "/opt/") {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	installed := filepath.Join(home, ".local", "share", "vantage")
	return strings.HasPrefix(exe, installed+string(os.PathSeparator)) || exe == filepath.Join(installed, "vantage")
}

func installDest() (dir, bin string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	dir = filepath.Join(home, ".local", "share", "vantage")
	return dir, filepath.Join(dir, "vantage"), nil
}

func sameFile(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = a
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = b
	}
	aa, _ := filepath.Abs(ra)
	bb, _ := filepath.Abs(rb)
	return aa == bb
}

func writeDesktopFiles(bin string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	iconDir := filepath.Join(home, ".local", "share", "icons", "hicolor", "scalable", "apps")
	if err := os.MkdirAll(iconDir, 0o755); err != nil {
		return err
	}
	iconPath := filepath.Join(iconDir, "vantage.svg")
	if err := os.WriteFile(iconPath, packaging.IconSVG, 0o644); err != nil {
		return err
	}
	appDir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return err
	}
	desktop := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Vantage
Comment=SSH, VNC, and RDP client
Exec=%s
Icon=%s
Terminal=false
Categories=Network;RemoteAccess;
StartupWMClass=vantage
`, bin, iconPath)
	desktopPath := filepath.Join(appDir, "vantage.desktop")
	if err := os.WriteFile(desktopPath, []byte(desktop), 0o644); err != nil {
		return err
	}
	_ = exec.Command("update-desktop-database", appDir).Run()
	return nil
}

func logInstall(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "vantage: "+format+"\n", args...)
}
