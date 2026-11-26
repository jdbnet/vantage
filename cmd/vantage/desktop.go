package main

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

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

// SaveDownload asks where to save a file, then copies it from the local API.
// apiPath is a same-app path such as /api/sftp/{id}/download?path=...
// The file body is not passed through the UI bridge, which rejects large calls.
// An empty path means the user cancelled.
func (d *Desktop) SaveDownload(defaultFilename, apiPath string) (string, error) {
	rawURL, err := desktopDownloadURL(apiPath)
	if err != nil {
		return "", err
	}
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
	resp, err := http.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		text := strings.TrimSpace(string(msg))
		if text == "" {
			text = resp.Status
		}
		return "", errors.New(text)
	}
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(path)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return "", closeErr
	}
	return path, nil
}

// desktopDownloadURL allows only a path on this app's desktop API.
func desktopDownloadURL(apiPath string) (string, error) {
	if !strings.HasPrefix(apiPath, "/api/") || strings.Contains(apiPath, "://") {
		return "", errors.New("invalid download")
	}
	if strings.ContainsAny(apiPath, "\r\n") {
		return "", errors.New("invalid download")
	}
	return "http://127.0.0.1:7688" + apiPath, nil
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

// ToggleFullscreen switches the window that handled this call.
// It returns whether that window is fullscreen afterwards.
func (d *Desktop) ToggleFullscreen() (bool, error) {
	win, err := currentWindow()
	if err != nil {
		return false, err
	}
	if win.IsFullscreen() {
		win.UnFullscreen()
		return false, nil
	}
	win.Fullscreen()
	return true, nil
}

// WindowIsFullscreen reports whether the window that handled this call is fullscreen.
func (d *Desktop) WindowIsFullscreen() (bool, error) {
	win, err := currentWindow()
	if err != nil {
		return false, err
	}
	return win.IsFullscreen(), nil
}

func currentWindow() (application.Window, error) {
	app := application.Get()
	if app == nil {
		return nil, errors.New("desktop shell not ready")
	}
	win := app.Window.Current()
	if win == nil {
		return nil, errors.New("no active window")
	}
	return win, nil
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
