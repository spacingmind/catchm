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
	"io/fs"
	"log"
	"os"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/spacingmind/catchm/internal/update"
)

// updateBaseURL is the update API root (ldflags-stamped by
// Taskfile.yml's desktop:release: -X main.updateBaseURL=<cloud
// origin>/update). Empty in dev builds — see newUpdateManager, which
// leaves the manager's client nil in that case so dev builds never
// hit the network.
var updateBaseURL = ""

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

// errUpdatesNotConfigured is returned by a manual check and by
// DownloadUpdate when this build has no update base URL (dev builds,
// or a build not stamped via desktop:release) — updates.client is nil
// in that case, and background checks never start.
var errUpdatesNotConfigured = errors.New("updates are not configured for this build")

// errDownloadInProgress: DownloadUpdate while another is still running.
// The frontend's shared store already prevents this; the check keeps
// two downloads from racing over m.verified if it ever doesn't.
var errDownloadInProgress = errors.New("an update download is already in progress")

// updateManager owns update state for the app's lifetime.
type updateManager struct {
	// client is nil when this build has no updateBaseURL (see
	// newUpdateManager): background checks never start, a manual
	// check reports errUpdatesNotConfigured, and DownloadUpdate
	// returns the same error.
	client *update.ReleaseClient

	mu          sync.Mutex
	lastChecked time.Time
	// lastStatus is the most recent check's result, so
	// OpenUpdateDownload can open the platform asset URL without a
	// re-check.
	lastStatus UpdateStatus
	// deferred is set when the user cancels UAC, dismisses the banner,
	// or a checksum mismatch occurs: update prompting stops for this
	// session (no retry loop, no nagging). A manual check from Settings
	// still always runs and reports.
	deferred bool
	// verified is the last successfully downloaded + checksum-verified
	// installer, ready for ApplyUpdate.
	verified update.VerifiedInstaller
	// dl is the in-flight DownloadUpdate, nil when idle — what
	// CancelDownload stops. One at a time: a second DownloadUpdate while
	// one runs gets errDownloadInProgress.
	dl *downloadRun
}

// downloadRun is one DownloadUpdate call's cancel handle. done closes
// once that call has cleaned up (partial file removed, dl cleared), so
// CancelDownload can return a state the user can retry from at once.
type downloadRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// cancelWait bounds how long CancelDownload waits for the cancelled
// download to unwind. A cancelled request returns almost immediately;
// this only guards the bridge call against a transport that ignores
// its context.
const cancelWait = 10 * time.Second

func newUpdateManager() *updateManager {
	base := updateBaseURL
	// Test hook: point the update client at a local server instead of
	// the real cloud origin (live end-to-end update trials per M23/M24's
	// spec — never publishes or fetches real releases). Also lets a dev
	// build opt into hitting a local Worker without an ldflags rebuild.
	if override := os.Getenv("CATCHM_UPDATE_BASE_URL"); override != "" {
		base = override
	}
	if base == "" {
		// Don't read the base from the user's config: sync can point at
		// a self-hosted server, but updates must come from the official
		// origin — a build with no stamped base simply has updates off.
		return &updateManager{}
	}
	return &updateManager{client: update.NewReleaseClient(base)}
}

// startBackgroundChecks runs the startup-delayed + 24h-periodic check
// loop until ctx is done. A no-op when this build has no update client
// (see newUpdateManager).
func (m *updateManager) startBackgroundChecks(ctx context.Context) {
	// Before the nil-client return: a build with updates off can still
	// have installers left over from one that had them.
	m.removeStaleInstallers()
	if m.client == nil {
		return
	}
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

// removeStaleInstallers deletes installers earlier runs downloaded but
// never applied (see update.RemoveStaleInstallers), keeping the one
// this run verified, if any. Holds m.mu throughout: a download
// registers itself under m.mu before creating its temp file, so with
// no download in flight no partial file can appear mid-sweep — and
// with one in flight the sweep waits for the next launch.
func (m *updateManager) removeStaleInstallers() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dl != nil {
		return
	}
	logUpdateErr("update: remove stale installers", update.RemoveStaleInstallers(m.verified.Path))
}

// checkAndEmit runs one check and emits updateAvailableEvent when a
// newer release is found — unless updates were deferred this session
// and this isn't a manual check.
func (m *updateManager) checkAndEmit(ctx context.Context, manual bool) UpdateStatus {
	return m.checkAndEmitWith(ctx, manual, func(status UpdateStatus) {
		if appCtx := appContext(); appCtx != nil {
			runtime.EventsEmit(appCtx, updateAvailableEvent, status)
		}
	})
}

// checkAndEmitWith is checkAndEmit with the emit seam injected so
// tests can exercise the manual/deferred matrix without a live Wails
// context (appContext is nil outside wails.Run).
func (m *updateManager) checkAndEmitWith(ctx context.Context, manual bool, emit func(UpdateStatus)) UpdateStatus {
	status := m.check(ctx)
	if !status.UpdateAvailable {
		return status
	}
	m.mu.Lock()
	deferred := m.deferred
	m.mu.Unlock()
	if deferred && !manual {
		return status
	}
	emit(status)
	return status
}

// check performs one check against the latest release, recording
// lastChecked. ErrNoRelease (fresh repo, nothing published yet) is not
// an error worth surfacing. When this build has no update client, it
// reports errUpdatesNotConfigured without recording lastChecked — a
// manual check from Settings shouldn't look like a check that ran.
func (m *updateManager) check(ctx context.Context) UpdateStatus {
	if m.client == nil {
		return UpdateStatus{CurrentVersion: version, SelfInstall: update.HostGOOS == "windows", Error: errUpdatesNotConfigured.Error()}
	}
	info, err := update.CheckForUpdate(ctx, m.client, version)
	status := UpdateStatus{
		CurrentVersion: version,
		SelfInstall:    update.HostGOOS == "windows",
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
		status.DownloadURL = info.DownloadURL
	}
	m.mu.Lock()
	m.lastChecked = time.Now()
	m.lastStatus = status
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
	// SelfInstall: this platform downloads and installs in-app
	// (Windows). Otherwise the UI offers OpenUpdateDownload instead.
	SelfInstall bool `json:"selfInstall"`
	// DownloadURL is the platform installer asset's URL when an update
	// is available (what OpenUpdateDownload opens on macOS).
	DownloadURL string `json:"downloadURL,omitempty"`
}

// CheckForUpdate (bound): manual check from Settings — same code path
// as the background check (checkAndEmit), no duplication. manual=true
// matters twice over: it bypasses this session's deferred flag (a
// manual check always reports), and it emits updateAvailableEvent so
// the banner appears alongside the returned status — the real
// v0.1.0→v0.1.2 trial showed that calling m.check directly here left
// Settings saying "Version v0.1.2 is available" with no actionable
// surface, because the banner only ever mounts on that event.
func (d DesktopBridge) CheckForUpdate() UpdateStatus {
	return d.updates.checkAndEmit(context.Background(), true)
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

// DownloadResult is what DownloadUpdate returns. Cancelled means
// CancelDownload stopped it: not an error (the UI says "Download
// cancelled." instead of showing a failure), SHA256 is empty and
// nothing was kept.
type DownloadResult struct {
	SHA256    string `json:"sha256"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

// OpenUpdateDownload (bound): macOS's update flow — the platform
// can't self-install (see update_other.go), so "Download" opens the
// last checked release's dmg URL in the browser. Errors when no check
// has found an update with a download URL yet.
func (d DesktopBridge) OpenUpdateDownload() error {
	d.updates.mu.Lock()
	url := d.updates.lastStatus.DownloadURL
	d.updates.mu.Unlock()
	if url == "" {
		return errors.New("no update download available — check for updates first")
	}
	ctx := appContext()
	if ctx == nil {
		return errors.New("app not running")
	}
	runtime.BrowserOpenURL(ctx, url)
	return nil
}

// DownloadUpdate (bound): downloads + SHA-256-verifies the installer
// for the latest release (re-checks rather than trusting a stale
// CheckForUpdate result, so the verified bytes always match the
// checksum published in the same release). Returns the verified
// installer's digest for the confirm dialog. A checksum mismatch marks
// updates deferred for the session — never executable, never retried.
func (d DesktopBridge) DownloadUpdate() (DownloadResult, error) {
	if update.HostGOOS != "windows" {
		return DownloadResult{}, errors.New("update: this platform can't self-install — use OpenUpdateDownload")
	}
	return d.updates.download(context.Background())
}

func (m *updateManager) download(parent context.Context) (DownloadResult, error) {
	if m.client == nil {
		return DownloadResult{}, errUpdatesNotConfigured
	}
	ctx, cancel := context.WithCancel(parent)
	run := &downloadRun{cancel: cancel, done: make(chan struct{})}
	m.mu.Lock()
	if m.dl != nil {
		m.mu.Unlock()
		cancel()
		return DownloadResult{}, errDownloadInProgress
	}
	m.dl = run
	// Whatever was verified before is stale now (declined with "Not
	// now", or superseded — this re-fetches the latest release):
	// delete it rather than leave tens of MB in temp.
	stale := m.verified.Path
	m.verified = update.VerifiedInstaller{}
	m.mu.Unlock()
	if stale != "" {
		if err := os.Remove(stale); err != nil && !errors.Is(err, fs.ErrNotExist) {
			logUpdateErr("update: remove previous installer", err)
		}
	}
	defer func() {
		m.mu.Lock()
		m.dl = nil
		m.mu.Unlock()
		cancel()
		close(run.done)
	}()

	// Checked via ctx.Err(), not the error: a cancelled request fails
	// with whatever the transport was doing at the time.
	rel, err := m.client.Latest(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return DownloadResult{Cancelled: true}, nil
		}
		return DownloadResult{}, err
	}
	verified, err := update.DownloadAndVerify(ctx, m.client, rel)
	if err != nil {
		// DownloadAndVerify already removed its temp file.
		if errors.Is(err, context.Canceled) {
			return DownloadResult{Cancelled: true}, nil
		}
		var mism *update.ChecksumMismatchError
		if errors.As(err, &mism) {
			m.mu.Lock()
			m.deferred = true
			m.mu.Unlock()
		}
		return DownloadResult{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// CancelDownload cancels under m.mu, so this check and the store
	// below are atomic with it: a cancel that lands after the bytes
	// verified still wins, and the file goes rather than sitting in
	// temp unreferenced.
	if ctx.Err() != nil {
		logUpdateErr("update: remove cancelled installer", os.Remove(verified.Path))
		return DownloadResult{Cancelled: true}, nil
	}
	m.verified = verified
	return DownloadResult{SHA256: verified.SHA256}, nil
}

// CancelDownload (bound): stops the in-flight DownloadUpdate, which
// then removes its partial file and resolves with Cancelled. Waits for
// that cleanup, so a retry straight after never hits
// errDownloadInProgress. A no-op when nothing is downloading.
func (d DesktopBridge) CancelDownload() {
	m := d.updates
	m.mu.Lock()
	run := m.dl
	if run != nil {
		run.cancel()
	}
	m.mu.Unlock()
	if run == nil {
		return
	}
	select {
	case <-run.done:
	case <-time.After(cancelWait):
		log.Printf("update: cancelled download still unwinding after %s", cancelWait)
	}
}

// ApplyUpdate (bound): launches the elevated update babysitter (one
// UAC prompt; the install is per-machine under Program Files, so
// elevation is unavoidable), then quits the app. The babysitter waits
// for this process to exit (releasing the exe lock NSIS /S silently
// fails against), runs the installer, and relaunches the updated exe
// unelevated — see update_windows.go for why the waiter must not be
// the replaced process (live-tested finding).
//
// The user canceling the UAC prompt is "update deferred", not an
// error: nil is returned and the deferred flag stops further prompting
// this session.
func (d DesktopBridge) ApplyUpdate() error {
	if update.HostGOOS != "windows" {
		return errors.New("update: this platform can't self-install — use OpenUpdateDownload")
	}
	m := d.updates
	m.mu.Lock()
	verified := m.verified
	m.mu.Unlock()
	if verified.Path == "" {
		return fmt.Errorf("no verified installer downloaded")
	}

	if err := launchUpdateBabysitter(verified.Path); err != nil {
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
