package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/catchm/internal/server"
)

type fakeExporter struct {
	file server.ExportFile
	err  error
}

func (f fakeExporter) Export(id, format, lang string) (server.ExportFile, error) {
	if f.err != nil {
		return server.ExportFile{}, f.err
	}
	return f.file, nil
}

func withDialog(t *testing.T, path string) {
	t.Helper()
	old := saveFileDialog
	saveFileDialog = func(defaultName, ext string) (string, error) {
		return path, nil
	}
	t.Cleanup(func() { saveFileDialog = old })
}

func TestSaveExportWritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	withDialog(t, path)

	b := &DesktopBridge{exp: fakeExporter{file: server.ExportFile{
		Body:     []byte("# hi"),
		Filename: "notes-2026-09-17.md",
	}}}
	got, err := b.SaveExport("m1", "md", "en")
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("path = %q, want %q", got, path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "# hi" {
		t.Fatalf("body = %q", body)
	}
}

func TestSaveExportCancel(t *testing.T) {
	withDialog(t, "")

	b := &DesktopBridge{exp: fakeExporter{file: server.ExportFile{Body: []byte("x")}}}
	got, err := b.SaveExport("m1", "txt", "en")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("path = %q, want empty", got)
	}
	if entries, _ := os.ReadDir(t.TempDir()); len(entries) != 0 {
		t.Fatalf("files written on cancel: %v", entries)
	}
}

func TestSaveExportAppendsExtension(t *testing.T) {
	dir := t.TempDir()
	withDialog(t, filepath.Join(dir, "my-notes"))

	b := &DesktopBridge{exp: fakeExporter{file: server.ExportFile{Body: []byte("# hi")}}}
	got, err := b.SaveExport("m1", "md", "en")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, ".md") {
		t.Fatalf("path = %q, want .md suffix", got)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatal(err)
	}
}

func TestSaveExportExportError(t *testing.T) {
	withDialog(t, filepath.Join(t.TempDir(), "x.txt"))

	b := &DesktopBridge{exp: fakeExporter{err: errors.New("boom")}}
	if _, err := b.SaveExport("m1", "txt", "en"); err == nil {
		t.Fatal("want error")
	}
}
