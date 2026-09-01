package guacx

import (
	"strings"
	"testing"

	"github.com/wwt/guac"
)

func TestErrorInstruction(t *testing.T) {
	t.Parallel()
	raw := ErrorInstruction("guacd connect: connection refused")
	ins, err := guac.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if ins.Opcode != "error" {
		t.Fatalf("opcode %q", ins.Opcode)
	}
	if len(ins.Args) != 2 {
		t.Fatalf("args %v", ins.Args)
	}
	if ins.Args[0] != "guacd connect: connection refused" {
		t.Fatalf("message %q", ins.Args[0])
	}
	if ins.Args[1] != "515" {
		t.Fatalf("code %q", ins.Args[1])
	}
}

func TestErrorInstructionEmpty(t *testing.T) {
	t.Parallel()
	ins, err := guac.Parse(ErrorInstruction("  "))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ins.Args[0], "display error") {
		t.Fatalf("got %q", ins.Args[0])
	}
}

func TestBuildConfigRDP(t *testing.T) {
	t.Parallel()
	cfg := buildConfig(Params{
		Protocol:   "rdp",
		Hostname:   "pc.example",
		Port:       3389,
		Width:      1440,
		Height:     900,
		ColorDepth: 24,
	})
	if cfg.OptimalScreenWidth != 1440 || cfg.OptimalScreenHeight != 900 {
		t.Fatalf("optimal size %d x %d", cfg.OptimalScreenWidth, cfg.OptimalScreenHeight)
	}
	if cfg.Parameters["width"] != "1440" || cfg.Parameters["height"] != "900" {
		t.Fatalf("rdp size params %#v", cfg.Parameters)
	}
	if cfg.Parameters["enable-gfx"] != "false" {
		t.Fatalf("enable-gfx=%q", cfg.Parameters["enable-gfx"])
	}
	if cfg.Parameters["resize-method"] != "display-update" {
		t.Fatalf("resize-method=%q", cfg.Parameters["resize-method"])
	}
	if len(cfg.ImageMimetypes) == 0 {
		t.Fatal("expected image mimetypes")
	}
}

func TestBuildConfigRDPDrive(t *testing.T) {
	t.Parallel()
	cfg := buildConfig(Params{
		Protocol:    "rdp",
		Hostname:    "pc.example",
		Port:        3389,
		EnableDrive: true,
		DrivePath:   "/data/shared",
		DriveName:   "Vantage",
	})
	if cfg.Parameters["enable-drive"] != "true" {
		t.Fatalf("enable-drive=%q", cfg.Parameters["enable-drive"])
	}
	if cfg.Parameters["drive-path"] != "/data/shared" {
		t.Fatalf("drive-path=%q", cfg.Parameters["drive-path"])
	}
	if cfg.Parameters["create-drive-path"] != "true" {
		t.Fatalf("create-drive-path=%q", cfg.Parameters["create-drive-path"])
	}
}

func TestBuildConfigVNCOmitsRDPFlags(t *testing.T) {
	t.Parallel()
	cfg := buildConfig(Params{Protocol: "vnc", Hostname: "vnc.example", Port: 5900})
	if _, ok := cfg.Parameters["enable-gfx"]; ok {
		t.Fatal("vnc should not set enable-gfx")
	}
	if cfg.Parameters["width"] != "1920" || cfg.Parameters["height"] != "1080" {
		t.Fatalf("default size %#v", cfg.Parameters)
	}
}
