package main

import (
	"context"
	"errors"
	"os"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type Desktop struct {
	ctx context.Context
}

func NewDesktop() *Desktop {
	return &Desktop{}
}

func (d *Desktop) setContext(ctx context.Context) {
	d.ctx = ctx
}

// SaveFile shows the native save dialog and writes data to the chosen path.
// Returns the saved path, or an empty string if the user cancelled.
func (d *Desktop) SaveFile(defaultFilename string, data []byte) (string, error) {
	if d.ctx == nil {
		return "", errors.New("desktop shell not ready")
	}
	if defaultFilename == "" {
		defaultFilename = "download"
	}
	path, err := runtime.SaveFileDialog(d.ctx, runtime.SaveDialogOptions{
		DefaultFilename: defaultFilename,
		Title:           "Save File",
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
