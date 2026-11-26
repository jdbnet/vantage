//go:build linux

package guacx

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DetectServerLayout reads the active XKB layout of this machine and returns
// the matching guacd server-layout name. guacd otherwise tells the RDP server
// the client keyboard is US English.
func DetectServerLayout() string {
	if layout, variant, ok := layoutFromSetxkbmap(); ok {
		return ServerLayout(layout, variant)
	}
	if layout, variant, ok := layoutFromGSettings(); ok {
		return ServerLayout(layout, variant)
	}
	if layout, variant, ok := layoutFromKxkbrc(); ok {
		return ServerLayout(layout, variant)
	}
	if layout, variant, ok := layoutFromLocalectl(); ok {
		return ServerLayout(layout, variant)
	}
	if layout, variant, ok := layoutFromDefaultKeyboard(); ok {
		return ServerLayout(layout, variant)
	}
	return ""
}

func layoutFromSetxkbmap() (string, string, bool) {
	out, ok := commandOutput("setxkbmap", "-query")
	if !ok {
		return "", "", false
	}
	return parseKeyValue(out, "layout:", "variant:")
}

func layoutFromGSettings() (string, string, bool) {
	out, ok := commandOutput("gsettings", "get", "org.gnome.desktop.input-sources", "mru-sources")
	if !ok || !strings.Contains(out, "xkb") {
		out, ok = commandOutput("gsettings", "get", "org.gnome.desktop.input-sources", "sources")
	}
	if !ok {
		return "", "", false
	}
	// [('xkb', 'gb+extd'), ('xkb', 'us')]
	i := strings.Index(out, "xkb")
	if i < 0 {
		return "", "", false
	}
	rest := out[i:]
	q1 := strings.Index(rest, "'")
	if q1 < 0 {
		return "", "", false
	}
	rest = rest[q1+1:]
	q2 := strings.Index(rest, "'")
	if q2 < 0 {
		return "", "", false
	}
	code := rest[:q2]
	layout, variant, _ := strings.Cut(code, "+")
	return layout, variant, layout != ""
}

func layoutFromKxkbrc() (string, string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", false
	}
	raw, err := os.ReadFile(home + "/.config/kxkbrc")
	if err != nil {
		return "", "", false
	}
	layout, _, ok := parseKeyValue(string(raw), "LayoutList=", "")
	if !ok {
		return "", "", false
	}
	layout, variant, _ := strings.Cut(layout, "(")
	variant = strings.TrimSuffix(variant, ")")
	return layout, variant, true
}

func layoutFromLocalectl() (string, string, bool) {
	out, ok := commandOutput("localectl", "status")
	if !ok {
		return "", "", false
	}
	return parseKeyValue(out, "X11 Layout:", "X11 Variant:")
}

func layoutFromDefaultKeyboard() (string, string, bool) {
	raw, err := os.ReadFile("/etc/default/keyboard")
	if err != nil {
		return "", "", false
	}
	text := string(raw)
	layout, _, ok := quotedConf(text, "XKBLAYOUT")
	if !ok {
		return "", "", false
	}
	variant, _, _ := quotedConf(text, "XKBVARIANT")
	return layout, variant, true
}

func parseKeyValue(text, layoutKey, variantKey string) (string, string, bool) {
	var layout, variant string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if layoutKey != "" && strings.HasPrefix(strings.ToLower(line), strings.ToLower(layoutKey)) {
			layout = strings.TrimSpace(line[len(layoutKey):])
		}
		if variantKey != "" && strings.HasPrefix(strings.ToLower(line), strings.ToLower(variantKey)) {
			variant = strings.TrimSpace(line[len(variantKey):])
		}
	}
	layout = strings.Trim(layout, `"'`)
	variant = strings.Trim(variant, `"'`)
	if layout == "" || layout == "-" {
		return "", "", false
	}
	return layout, variant, true
}

func quotedConf(text, key string) (string, string, bool) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || !strings.HasPrefix(line, key) {
			continue
		}
		_, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if value == "" {
			return "", "", false
		}
		return value, "", true
	}
	return "", "", false
}

func commandOutput(name string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return "", false
	}
	return buf.String(), true
}
