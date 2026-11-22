package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/jdbnet/vantage/internal/appcore"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func main() {
	maybeSelfInstall()

	dataDir := desktopDataDir()
	core, err := appcore.Open(dataDir, "desktop", "127.0.0.1:7688", false)
	if err != nil {
		log.Fatal(err)
	}
	defer core.Close()
	core.StartSyncClient()

	go func() {
		log.Printf("vantage desktop API on %s (data %s)", core.Listen, dataDir)
		if err := http.ListenAndServe(core.Listen, core.Handler()); err != nil {
			log.Printf("local http: %v", err)
		}
	}()

	desktop := NewDesktop()
	app := application.New(application.Options{
		Name:        "Vantage",
		Description: "SSH, VNC, and RDP client",
		Services: []application.Service{
			application.NewService(desktop),
		},
		Assets: application.AssetOptions{
			Handler: core.Handler(),
		},
		OnShutdown: func() {
			_ = core.Close()
		},
	})

	if runtime.GOOS == "darwin" {
		appMenu := app.Menu.New()
		appMenu.AddRole(application.AppMenu)
		appMenu.AddRole(application.EditMenu)
		app.Menu.Set(appMenu)
	}

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Vantage",
		Width:            1400,
		Height:           900,
		MinWidth:         800,
		MinHeight:        600,
		MaxWidth:         16384,
		MaxHeight:        16384,
		BackgroundColour: application.NewRGB(13, 17, 23),
	})
	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		enableWebkitPopouts()
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func desktopDataDir() string {
	if d := os.Getenv("VANTAGE_DATA_DIR"); d != "" {
		return d
	}
	switch runtime.GOOS {
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "vantage")
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, "Library", "Application Support", "vantage")
		}
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "vantage")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "./data"
	}
	return filepath.Join(home, ".local", "share", "vantage")
}
