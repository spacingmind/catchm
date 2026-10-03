// tray.go implements M22 §3's tray polish: icon states (idle/
// recording/error), a tooltip that never leaks a meeting title (screen-
// share privacy — docs/brand-and-ux-research.md), left-click showing
// the window, and a menu with Open / Start-stop meeting / Compact mode
// / Quit. Systray API calls verified against the actual v1.0.3 source
// in the module cache (same standard M11 set for this file): Click,
// SetTitle, SetIcon/SetTooltip (package-level, for the tray icon
// itself), and SetOnClick (its doc comment, in Chinese in the source,
// translates to "set the tray's left mouse button click event" — right-
// click already shows the menu by default when SetOnRClick is never
// called, so no code is needed for that half of "left-click shows the
// window, [right-click for] a menu").
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/meeting"
)

// How often the tray re-checks meeting/engine state.
const tooltipPollInterval = 3 * time.Second

// trayToggleCompactEvent is the Wails event the tray's "Compact mode"
// item emits; web/src/components/live/useCompactMode.ts subscribes to
// this exact name via window.runtime.EventsOn.
const trayToggleCompactEvent = "tray:toggle-compact"

// Systray's SetIcon on Windows wants raw .ico file bytes (per its own
// doc comment, confirmed in M11), one per tray state.
//
//go:embed build/trayicon.ico
var trayIconIdle []byte

//go:embed build/trayicon_recording.ico
var trayIconRecording []byte

//go:embed build/trayicon_error.ico
var trayIconError []byte

var mStartStop *systray.MenuItem

// trayState is what the tray currently shows, derived from real signals
// only: an active (un-ended) meeting from GET /api/meetings, and — for
// "error" specifically — the M21 §2 engine-state field from GET
// /api/meetings/{id}/levels. There is no persisted capture_error to
// poll (it's an ephemeral field only ever returned once, from the
// start/source-switch responses — see internal/server/server.go), so
// "error" only reflects a LIVE engine failure, not a historical one;
// anything else with an active meeting reads as "recording", not
// invented into a state the backend can't actually report.
type trayState struct {
	label     string // "idle" | "recording" | "error"
	meetingID string
	errMsg    string
}

// onTrayReady builds the tray menu and starts the state poll. It runs
// inside systray.Run's event loop startup.
func onTrayReady(ctx context.Context, handler http.Handler, cfg *config.Config, hkManager *hotkeyManager) {
	systray.SetIcon(trayIconIdle)
	systray.SetTooltip("CatchM — Ready")
	systray.SetOnClick(func(systray.IMenu) {
		if appCtx := appContext(); appCtx != nil {
			windowVisible.Store(true)
			runtime.WindowShow(appCtx)
		}
	})

	mOpen := systray.AddMenuItem("Open CatchM", "Show the CatchM window")
	mOpen.Click(func() {
		if appCtx := appContext(); appCtx != nil {
			windowVisible.Store(true)
			runtime.WindowShow(appCtx)
		}
	})

	mStartStop = systray.AddMenuItem("Start meeting", "Start or stop the current meeting")
	mStartStop.Click(func() { onTrayStartStopClick(handler, cfg) })

	mCompact := systray.AddMenuItem("Compact mode", "Toggle the compact reading window")
	mCompact.Click(func() {
		if appCtx := appContext(); appCtx != nil {
			runtime.EventsEmit(appCtx, trayToggleCompactEvent)
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

	go pollTrayState(ctx, handler, hkManager)
}

// pollTrayState keeps the tray icon, tooltip, and start/stop menu label
// honest about real capture state (M11's core lesson: never let the
// tray look idle or recording when it isn't), and — M22 §5 — fires a
// toast for the two transitions that matter while the window is
// hidden: capture starting to error, and the active meeting ending.
func pollTrayState(ctx context.Context, handler http.Handler, hkManager *hotkeyManager) {
	ticker := time.NewTicker(tooltipPollInterval)
	defer ticker.Stop()
	var prev trayState
	haveBaseline := false
	for {
		cur := currentTrayState(handler)
		applyTrayState(cur)
		if haveBaseline {
			notifyTrayTransition(hkManager, prev, cur)
		}
		prev, haveBaseline = cur, true
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// notifyTrayTransition toasts on the two edges the spec calls out —
// never on the steady state (only checked once haveBaseline is true,
// above, so launching with a meeting already active from a prior
// session doesn't toast on the very first poll). Toasts only matter
// while the window is hidden — the whole point is surfacing state the
// user can't otherwise see.
func notifyTrayTransition(hkManager *hotkeyManager, prev, cur trayState) {
	if windowVisible.Load() {
		return
	}
	if prev.label != "error" && cur.label == "error" {
		msg := cur.errMsg
		if msg == "" {
			msg = "capture stopped"
		}
		// No meeting id here: the meeting is still live and the live
		// view is already where this error surfaces.
		notify(hkManager, "CatchM", "Not recording: "+msg, "")
	}
	// The previously-active meeting is no longer active: it ended (or a
	// different meeting became active via the web UI/MCP tools — either
	// way the old one isn't live anymore). No title (screen-share
	// privacy, same rule as the tooltip). The toast carries the ended
	// meeting's id so clicking it opens that meeting (activate.go's
	// catchm://meeting/<id> protocol activation).
	if prev.meetingID != "" && cur.meetingID != prev.meetingID {
		notify(hkManager, "CatchM", "Meeting ended", prev.meetingID)
	}
}

// applyTrayState is a no-op on darwin: there is no tray there (see
// tray_start_darwin.go) and the menu items below were never created.
func applyTrayState(s trayState) {
	if goosDarwin {
		return
	}
	switch s.label {
	case "recording":
		systray.SetIcon(trayIconRecording)
		systray.SetTooltip("CatchM — Recording")
		mStartStop.SetTitle("Stop meeting")
	case "error":
		systray.SetIcon(trayIconError)
		msg := s.errMsg
		if msg == "" {
			msg = "capture stopped"
		}
		// No meeting title in the tooltip (screen-share privacy).
		systray.SetTooltip("CatchM — Not recording: " + msg)
		mStartStop.SetTitle("Stop meeting")
	default:
		systray.SetIcon(trayIconIdle)
		systray.SetTooltip("CatchM — Ready")
		mStartStop.SetTitle("Start meeting")
	}
}

func onTrayStartStopClick(handler http.Handler, cfg *config.Config) {
	s := currentTrayState(handler)
	if s.meetingID != "" {
		endMeeting(handler, s.meetingID)
		return
	}
	startMeeting(handler, cfg)
}

func currentTrayState(handler http.Handler) trayState {
	id, active, err := activeMeeting(handler)
	if err != nil || !active {
		return trayState{label: "idle"}
	}
	state, errMsg, ok := engineState(handler, id)
	if ok && state == "error" {
		return trayState{label: "error", meetingID: id, errMsg: errMsg}
	}
	return trayState{label: "recording", meetingID: id}
}

// activeMeeting finds the meeting with no ended_at, the same rule M11's
// tooltip poll (and the web app's own live indicator) already use.
func activeMeeting(handler http.Handler) (id string, active bool, err error) {
	req, err := http.NewRequest(http.MethodGet, "/api/meetings", nil)
	if err != nil {
		return "", false, err
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return "", false, fmt.Errorf("list meetings: status %d", rec.Code)
	}
	var meetings []meeting.Meeting
	if err := json.Unmarshal(rec.Body.Bytes(), &meetings); err != nil {
		return "", false, err
	}
	for _, m := range meetings {
		if m.EndedAt == nil {
			return m.ID, true, nil
		}
	}
	return "", false, nil
}

// engineState reads the M21 §2 engine.state field from the live-levels
// endpoint. ok is false when the pipeline doesn't exist (meeting just
// ended) or the engine doesn't implement the status reporter — neither
// is an error, just "nothing to report", matching that endpoint's own
// contract.
func engineState(handler http.Handler, meetingID string) (state, errMsg string, ok bool) {
	req, err := http.NewRequest(http.MethodGet, "/api/meetings/"+meetingID+"/levels", nil)
	if err != nil {
		return "", "", false
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return "", "", false
	}
	var body struct {
		Engine *struct {
			State string `json:"state"`
			Error string `json:"error"`
		} `json:"engine"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Engine == nil {
		return "", "", false
	}
	return body.Engine.State, body.Engine.Error, true
}

// startMeeting mirrors StartMeetingPopover's own auto-generated title
// (web/src/components/StartMeetingPopover.tsx's defaultTitle) since the
// tray has no UI to ask for one, using the configured default engine so
// capture actually starts (an empty engine creates a title-only meeting
// with no live capture at all — see handleStartMeeting).
func startMeeting(handler http.Handler, cfg *config.Config) {
	reqBody, err := json.Marshal(map[string]any{
		"title":  "Meeting " + time.Now().Format("Jan 2, 3:04 PM"),
		"engine": cfg.Engine,
		"source": "mic",
	})
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, "/api/meetings", bytes.NewReader(reqBody))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(httptest.NewRecorder(), req)
}

func endMeeting(handler http.Handler, meetingID string) {
	req, err := http.NewRequest(http.MethodPost, "/api/meetings/"+meetingID+"/end", nil)
	if err != nil {
		return
	}
	handler.ServeHTTP(httptest.NewRecorder(), req)
}
