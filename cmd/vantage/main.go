package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jdbnet/vantage/internal/appcore"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
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

	err = wails.Run(&options.App{
		Title:  "Vantage",
		Width:  1400,
		Height: 900,
		AssetServer: &assetserver.Options{
			Handler: core.Handler(),
		},
		BackgroundColour: &options.RGBA{R: 13, G: 17, B: 23, A: 255},
		OnShutdown: func(ctx context.Context) {
			_ = core.Close()
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

func desktopDataDir() string {
	if d := os.Getenv("VANTAGE_DATA_DIR"); d != "" {
		return d
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
