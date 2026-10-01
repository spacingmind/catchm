package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/spacingmind/catchm/internal/server"
)

// meetingExporter is the slice of *server.Server the export path needs.
type meetingExporter interface {
	Export(id, format, lang string) (server.ExportFile, error)
}

// saveFileDialog is swapped out in tests.
var saveFileDialog = func(defaultName, ext string) (string, error) {
	ctx := appContext()
	if ctx == nil {
		return "", nil
	}
	display := map[string]string{
		"txt":  "Text",
		"md":   "Markdown",
		"docx": "Word document",
	}[ext]
	return runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
		DefaultFilename: defaultName,
		Filters:         []runtime.FileFilter{{DisplayName: display, Pattern: "*." + ext}},
	})
}

// SaveExport exports a meeting and writes it to a user-chosen path.
// WKWebView/Wails ignores web downloads, so the desktop shell saves
// natively instead of letting the frontend trigger a browser download.
func (b *DesktopBridge) SaveExport(meetingID, format, lang string) (string, error) {
	f, err := b.exp.Export(meetingID, format, lang)
	if err != nil {
		return "", err
	}
	path, err := saveFileDialog(f.Filename, format)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	if !strings.EqualFold(filepath.Ext(path), "."+format) {
		path += "." + format
	}
	if err := os.WriteFile(filepath.Clean(path), f.Body, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
