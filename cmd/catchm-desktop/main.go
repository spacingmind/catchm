// Command catchm-desktop opens a native desktop window showing the same
// web UI and API that `catchm serve` exposes over HTTP, reusing
// internal/server's http.Handler directly as the window's asset source
// (no separate frontend, no TCP port, no code fork). A system tray icon
// keeps the app alive in the background when the window is closed, so an
// in-progress capture survives; the tray's Quit item is the only way the
// app actually exits.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/server"
	"github.com/spacingmind/catchm/internal/tools"
)

// How often the tray tooltip re-checks for an active (un-ended) meeting.
const tooltipPollInterval = 3 * time.Second

// Systray's SetIcon on Windows wants raw .ico file bytes, so the mascot
// art is shipped as a pre-sized 16/32px ICO next to appicon.png.
//
//go:embed build/trayicon.ico
var trayIcon []byte

// Shared between the Wails callbacks and the systray goroutine: the
// runtime context from OnStartup, and the flag the tray's Quit item
// sets so OnBeforeClose lets the real close through.
var (
	mu         sync.Mutex
	wailsCtx   context.Context
	reallyQuit bool
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

	pollCtx, stopPoll := context.WithCancel(context.Background())
	defer stopPoll()

	go systray.Run(
		func() { onTrayReady(pollCtx, handler) },
		stopPoll,
	)

	return wails.Run(&options.App{
		Title:  "CatchM",
		Width:  1180,
		Height: 760,
		AssetServer: &assetserver.Options{
			Handler: handler,
		},
		OnStartup: func(ctx context.Context) {
			mu.Lock()
			wailsCtx = ctx
			mu.Unlock()
		},
		OnBeforeClose: func(ctx context.Context) (prevent bool) {
			mu.Lock()
			quit := reallyQuit
			mu.Unlock()
			if quit {
				return false
			}
			runtime.WindowHide(ctx)
			return true
		},
	})
}

// onTrayReady builds the tray menu and starts the tooltip poll. It runs
// inside systray.Run's event loop startup.
func onTrayReady(ctx context.Context, handler http.Handler) {
	systray.SetIcon(trayIcon)
	systray.SetTooltip("CatchM")

	mOpen := systray.AddMenuItem("Open CatchM", "Show the CatchM window")
	mOpen.Click(func() {
		if appCtx := appContext(); appCtx != nil {
			runtime.WindowShow(appCtx)
		}
	})

	mQuit := systray.AddMenuItem("Quit", "Quit CatchM (ends any in-progress capture)")
	mQuit.Click(func() {
		mu.Lock()
		reallyQuit = true
		mu.Unlock()
		systray.Quit()
		if appCtx := appContext(); appCtx != nil {
			runtime.Quit(appCtx)
		}
	})

	go pollActiveMeeting(ctx, handler)
}

func appContext() context.Context {
	mu.Lock()
	defer mu.Unlock()
	return wailsCtx
}

// pollActiveMeeting keeps the tray tooltip honest about capture state:
// "CatchM — Recording" while any meeting is un-ended, plain "CatchM"
// otherwise. It serves GET /api/meetings through the same in-process
// handler the window uses, so no HTTP listener is involved.
func pollActiveMeeting(ctx context.Context, handler http.Handler) {
	ticker := time.NewTicker(tooltipPollInterval)
	defer ticker.Stop()
	for {
		recording, err := anyMeetingActive(handler)
		if err == nil {
			if recording {
				systray.SetTooltip("CatchM — Recording")
			} else {
				systray.SetTooltip("CatchM")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func anyMeetingActive(handler http.Handler) (bool, error) {
	req, err := http.NewRequest(http.MethodGet, "/api/meetings", nil)
	if err != nil {
		return false, err
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return false, fmt.Errorf("list meetings: status %d", rec.Code)
	}
	var meetings []meeting.Meeting
	if err := json.Unmarshal(rec.Body.Bytes(), &meetings); err != nil {
		return false, err
	}
	for _, m := range meetings {
		if m.EndedAt == nil {
			return true, nil
		}
	}
	return false, nil
}
