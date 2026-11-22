package main

import (
	"errors"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type Desktop struct{}

func NewDesktop() *Desktop {
	return &Desktop{}
}

// SaveFile shows the native save dialog and writes data to the chosen path.
// Returns the saved path, or an empty string if the user cancelled.
func (d *Desktop) SaveFile(defaultFilename string, data []int) (string, error) {
	app := application.Get()
	if app == nil {
		return "", errors.New("desktop shell not ready")
	}
	if defaultFilename == "" {
		defaultFilename = "download"
	}
	path, err := app.Dialog.SaveFile().
		SetMessage("Save File").
		SetFilename(defaultFilename).
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	buf := make([]byte, len(data))
	for i, n := range data {
		buf[i] = byte(n)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// ReadClipboard returns the host system clipboard as plain text.
func (d *Desktop) ReadClipboard() (string, error) {
	app := application.Get()
	if app == nil {
		return "", errors.New("desktop shell not ready")
	}
	text, ok := app.Clipboard.Text()
	if !ok {
		return "", errors.New("clipboard text unavailable")
	}
	return text, nil
}

// WriteClipboard sets the host system clipboard to plain text.
func (d *Desktop) WriteClipboard(text string) error {
	app := application.Get()
	if app == nil {
		return errors.New("desktop shell not ready")
	}
	if !app.Clipboard.SetText(text) {
		return errors.New("clipboard write failed")
	}
	return nil
}
