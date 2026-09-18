// Command catchm-desktop opens a native desktop window showing the same
// web UI and API that `catchm serve` exposes over HTTP, reusing
// internal/server's http.Handler directly as the window's asset source
// (no separate frontend, no TCP port, no code fork).
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/server"
	"github.com/spacingmind/catchm/internal/tools"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	home := os.Getenv("CATCHM_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home dir: %w", err)
		}
		home = filepath.Join(userHome, ".catchm")
	}

	store, err := meeting.NewStore(filepath.Join(home, "meetings"))
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	handler := server.New(tools.New(store), cfg)

	return wails.Run(&options.App{
		Title:  "CatchM",
		Width:  1180,
		Height: 760,
		AssetServer: &assetserver.Options{
			Handler: handler,
		},
	})
}
