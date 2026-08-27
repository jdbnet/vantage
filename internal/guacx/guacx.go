package guacx

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/wwt/guac"
)

type Params struct {
	GuacdAddr   string
	Protocol    string
	Hostname    string
	Port        int
	Username    string
	Password    string
	Domain      string
	Width       int
	Height      int
	ColorDepth  int
	EnableDrive bool
	DrivePath   string
	DriveName   string
}

func Open(p Params) (guac.Tunnel, error) {
	if p.GuacdAddr == "" {
		return nil, fmt.Errorf("guacd address is not configured")
	}
	cfg := buildConfig(p)

	addr, err := net.ResolveTCPAddr("tcp", p.GuacdAddr)
	if err != nil {
		return nil, fmt.Errorf("guacd address: %w", err)
	}
	conn, err := net.DialTimeout("tcp", addr.String(), 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("guacd connect: %w", err)
	}
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("guacd: expected tcp")
	}
	stream := guac.NewStream(tcp, guac.SocketTimeout)
	if err := stream.Handshake(cfg); err != nil {
		_ = tcp.Close()
		return nil, fmt.Errorf("guacd handshake: %w", err)
	}
	return guac.NewSimpleTunnel(stream), nil
}

func buildConfig(p Params) *guac.Config {
	if p.Width <= 0 {
		p.Width = 1920
	}
	if p.Height <= 0 {
		p.Height = 1080
	}
	if p.ColorDepth <= 0 {
		p.ColorDepth = 24
	}
	cfg := guac.NewGuacamoleConfiguration()
	cfg.Protocol = p.Protocol
	cfg.OptimalScreenWidth = p.Width
	cfg.OptimalScreenHeight = p.Height
	cfg.OptimalResolution = 96
	cfg.ImageMimetypes = []string{"image/png", "image/jpeg"}
	cfg.Parameters["hostname"] = p.Hostname
	cfg.Parameters["port"] = strconv.Itoa(p.Port)
	cfg.Parameters["width"] = strconv.Itoa(p.Width)
	cfg.Parameters["height"] = strconv.Itoa(p.Height)
	cfg.Parameters["dpi"] = "96"
	cfg.Parameters["color-depth"] = strconv.Itoa(p.ColorDepth)
	cfg.Parameters["ignore-cert"] = "true"
	if p.Username != "" {
		cfg.Parameters["username"] = p.Username
	}
	if p.Password != "" {
		cfg.Parameters["password"] = p.Password
	}
	if p.Domain != "" {
		cfg.Parameters["domain"] = p.Domain
	}
	if p.Protocol == "rdp" {
		// GFX/H.264 often paints the Windows logon UI then leaves the desktop black
		// in the HTML5 client. Bitmap updates remain visible after Duo.
		cfg.Parameters["enable-gfx"] = "false"
		cfg.Parameters["resize-method"] = "display-update"
		cfg.Parameters["enable-wallpaper"] = "true"
		cfg.Parameters["enable-theming"] = "true"
		cfg.Parameters["enable-font-smoothing"] = "true"
		if p.EnableDrive && p.DrivePath != "" {
			cfg.Parameters["enable-drive"] = "true"
			cfg.Parameters["drive-path"] = p.DrivePath
			name := p.DriveName
			if name == "" {
				name = "Vantage"
			}
			cfg.Parameters["drive-name"] = name
			cfg.Parameters["create-drive-path"] = "true"
		}
	}
	return cfg
}

func ErrorInstruction(msg string) []byte {
	if strings.TrimSpace(msg) == "" {
		msg = "display error"
	}
	code := strconv.Itoa(guac.UpstreamError.GetGuacamoleStatusCode())
	return guac.NewInstruction("error", msg, code).Byte()
}
