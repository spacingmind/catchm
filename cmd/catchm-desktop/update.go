// update.go: M23's Wails-bound update orchestration — the only file in
// the update path that knows about runtime.EventsEmit / bound methods.
// Everything security-relevant (checksum verification) lives in
// internal/update; this file decides WHEN to check and HOW to hand the
// verified installer to Windows. The elevation/relaunch syscalls are
// in update_windows.go (update_other.go stubs them so CI on Linux
// still compiles this package).
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/spacingmind/catchm/internal/update"
)

// updateRepo is the GitHub repo releases are published to
// (.github/workflows/release.yml).
const updateRepo = "spacingmind/catchm"

// Wails events the frontend subscribes to (window.runtime.EventsOn):
const (
	// updateAvailableEvent fires when a check (background or manual)
	// finds a newer release; payload: *UpdateStatus — the same shape
	// CheckForUpdate returns, so the Settings page and the banner share
	// one rendering path.
	updateAvailableEvent = "update:available"
)

// Background-check timing (spec): one check a few seconds after
// startup (not blocking window show) and once per ~24h while running.
const (
	updateStartupDelay = 10 * time.Second
	updateInterval     = 24 * time.Hour
)

// updateManager owns update state for the app's lifetime.
type updateManager struct {
	client *update.GitHubClient

	mu          sync.Mutex
	lastChecked time.Time
	// deferred is set when the user cancels UAC, dismisses the banner,
	// or a checksum mismatch occurs: update prompting stops for this
	// session (no retry loop, no nagging). A manual check from Settings
	// still always runs and reports.
	deferred bool
	// verified is the last successfully downloaded + checksum-verified
	// installer, ready for ApplyUpdate.
	verified update.VerifiedInstaller
}

func newUpdateManager() *updateManager {
	c := update.NewGitHubClient(updateRepo)
	// Test hook: point the update client at a local server instead of
	// the real GitHub API (live end-to-end update trials per M23's spec
	// — never publishes or fetches real releases). Empty in production.
	if base := os.Getenv("CATCHM_UPDATE_BASE_URL"); base != "" {
		c.BaseURL = base
	}
	return &updateManager{client: c}
}

// startBackgroundChecks runs the startup-delayed + 24h-periodic check
// loop until ctx is done.
func (m *updateManager) startBackgroundChecks(ctx context.Context) {
	go func() {
		time.Sleep(updateStartupDelay)
		m.checkAndEmit(ctx, false)
		t := time.NewTicker(updateInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.checkAndEmit(ctx, false)
			}
		}
	}()
}

// checkAndEmit runs one check and emits updateAvailableEvent when a
// newer release is found — unless updates were deferred this session
// and this isn't a manual check.
func (m *updateManager) checkAndEmit(ctx context.Context, manual bool) {
	status := m.check(ctx)
	if !status.UpdateAvailable {
		return
	}
	m.mu.Lock()
	deferred := m.deferred
	m.mu.Unlock()
	if deferred && !manual {
		return
	}
	if appCtx := appContext(); appCtx != nil {
		runtime.EventsEmit(appCtx, updateAvailableEvent, status)
	}
}

// check performs one check against the latest release, recording
// lastChecked. ErrNoRelease (fresh repo, nothing published yet) is not
// an error worth surfacing.
func (m *updateManager) check(ctx context.Context) UpdateStatus {
	info, err := update.CheckForUpdate(ctx, m.client, version)
	status := UpdateStatus{
		CurrentVersion: version,
		CheckedAt:      time.Now().Format(time.RFC3339),
	}
	if err != nil {
		if !errors.Is(err, update.ErrNoRelease) {
			status.Error = err.Error()
		}
	} else if info != nil {
		status.UpdateAvailable = true
		status.LatestVersion = info.Version
		status.Notes = info.Notes
		status.NotesURL = info.NotesURL
		status.PublishedAt = info.PublishedAt.Format("2006-01-02")
	}
	m.mu.Lock()
	m.lastChecked = time.Now()
	m.mu.Unlock()
	return status
}

// UpdateStatus is the bound-method / event payload shape for the
// frontend (Settings page and banner).
type UpdateStatus struct {
	CurrentVersion  string `json:"currentVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	LatestVersion   string `json:"latestVersion,omitempty"`
	Notes           string `json:"notes,omitempty"`
	NotesURL        string `json:"notesURL,omitempty"`
	PublishedAt     string `json:"publishedAt,omitempty"`
	CheckedAt       string `json:"checkedAt,omitempty"`
	Error           string `json:"error,omitempty"`
}

// CheckForUpdate (bound): manual check from Settings — same code path
// as the background check, no duplication.
func (d DesktopBridge) CheckForUpdate() UpdateStatus {
	return d.updates.check(context.Background())
}

// GetLastChecked (bound): RFC3339 timestamp of the last check of any
// kind ("" if none ran yet), for the Settings page.
func (d DesktopBridge) GetLastChecked() string {
	d.updates.mu.Lock()
	defer d.updates.mu.Unlock()
	if d.updates.lastChecked.IsZero() {
		return ""
	}
	return d.updates.lastChecked.Format(time.RFC3339)
}

// DownloadResult is what DownloadUpdate returns.
type DownloadResult struct {
	SHA256 string `json:"sha256"`
}

// DownloadUpdate (bound): downloads + SHA-256-verifies the installer
// for the latest release (re-checks rather than trusting a stale
// CheckForUpdate result, so the verified bytes always match the
// checksum published in the same release). Returns the verified
// installer's digest for the confirm dialog. A checksum mismatch marks
// updates deferred for the session — never executable, never retried.
func (d DesktopBridge) DownloadUpdate() (DownloadResult, error) {
	m := d.updates
	ctx := context.Background()

	rel, err := m.client.Latest(ctx)
	if err != nil {
		return DownloadResult{}, err
	}
	verified, err := update.DownloadAndVerify(ctx, m.client, rel)
	if err != nil {
		var mism *update.ChecksumMismatchError
		if errors.As(err, &mism) {
			m.mu.Lock()
			m.deferred = true
			m.mu.Unlock()
		}
		return DownloadResult{}, err
	}

	m.mu.Lock()
	m.verified = verified
	m.mu.Unlock()
	return DownloadResult{SHA256: verified.SHA256}, nil
}

// ApplyUpdate (bound): launches the verified installer elevated (the
// install is per-machine under Program Files, so UAC is unavoidable)
// with NSIS's silent flag, then quits the app so the installer can
// replace the running files. The actual relaunch of the updated app is
// finished in main() after wails.Run returns — see
// finishUpdateRelaunch (update_windows.go) for the where/why.
//
// The user canceling the UAC prompt is "update deferred", not an
// error: nil is returned and the deferred flag stops further prompting
// this session.
func (d DesktopBridge) ApplyUpdate() error {
	m := d.updates
	m.mu.Lock()
	verified := m.verified
	m.mu.Unlock()
	if verified.Path == "" {
		return fmt.Errorf("no verified installer downloaded")
	}

	if err := launchInstallerElevated(verified.Path); err != nil {
		if errors.Is(err, errUACCanceled) {
			m.mu.Lock()
			m.deferred = true
			m.mu.Unlock()
			return nil
		}
		return err
	}

	// Quit so the installer can replace our files — same path as the
	// tray Quit item (reallyQuit lets OnBeforeClose through). Unlike
	// the tray item we don't systray.Quit from here; the installer is
	// already running and main() finishes the sequence.
	mu.Lock()
	reallyQuit = true
	mu.Unlock()
	if appCtx := appContext(); appCtx != nil {
		runtime.Quit(appCtx)
	}
	return nil
}

// DeferUpdate (bound): the user dismissed the banner / declined the
// confirm — stop prompting this session.
func (d DesktopBridge) DeferUpdate() {
	d.updates.mu.Lock()
	d.updates.deferred = true
	d.updates.mu.Unlock()
}

// GetAppVersion (bound): the running version for the Settings page.
func (DesktopBridge) GetAppVersion() string { return version }

// errUACCanceled: Windows returns ERROR_CANCELLED when the user
// dismisses the elevation prompt.
var errUACCanceled = errors.New("update: UAC prompt canceled by user")

// Log update-loop errors without crashing anything — the app must keep
// working perfectly whether updates are available or not.
func logUpdateErr(prefix string, err error) {
	if err != nil {
		log.Printf("%s: %v", prefix, err)
	}
}

var _ = logUpdateErr // used by update_windows.go paths; keep linters honest on stub builds
