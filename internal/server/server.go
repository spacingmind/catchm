// Package server exposes the meeting tool surface over HTTP and serves
// the embedded web UI.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/spacingmind/catchm/internal/batch"
	"github.com/spacingmind/catchm/internal/capture"
	"github.com/spacingmind/catchm/internal/cloudsync"
	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/mcpserver"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/pipeline"
	"github.com/spacingmind/catchm/internal/stt"
	"github.com/spacingmind/catchm/internal/summarize"
	"github.com/spacingmind/catchm/internal/sysinfo"
	"github.com/spacingmind/catchm/internal/tools"
	"github.com/spacingmind/catchm/internal/translate"
)

// Server serves the CatchM HTTP API.
type Server struct {
	surface *tools.Surface
	cfg     *config.Config
	mux     *http.ServeMux

	newEngine func(name string, cfg *config.Config) (stt.StreamEngine, error)
	newSource func(device string) (capture.Source, error)
	// System-audio seams, so "both"/"system" paths are testable without
	// a real loopback/tap.
	systemSupported func() (bool, string)
	newSystemSource func() (capture.Source, error)
	newBatchEngine  func(cfg *config.Config, engine string) (stt.BatchEngine, error)
	newTranslator   func(cfg *config.Config, model string) (translate.Translator, error)
	newSummarizer   func(cfg *config.Config, cloud summarize.CloudAuth) (summarize.Summarizer, error)
	// startSonioxCleanup runs once at construction when a Soniox key
	// is set; a test seam so no network is touched in tests.
	startSonioxCleanup func()

	mu        sync.Mutex
	pipelines map[string]*pipeline.Pipeline

	// cloud is the in-memory-only cloud-sync session; nil when logged
	// out. See cloud.go.
	cloud   *cloudState
	syncNow func() (*cloudsync.SyncResult, error)
	// newCloudClient is a seam for tests to swap in a fake backend.
	newCloudClient func() *cloudsync.Client
	// mcpHTTP is the local MCP HTTP transport (docs/plans/completed/local-mcp.md).
	mcpHTTP *mcpserver.HTTPServer
}

// New builds a Server over the tool surface. cfg may be nil, in which case
// engine defaults apply as if no config file were present.
func New(surface *tools.Surface, cfg *config.Config) *Server {
	if cfg == nil {
		cfg = &config.Config{}
	}
	s := &Server{
		surface:         surface,
		cfg:             cfg,
		mux:             http.NewServeMux(),
		newEngine:       newEngine,
		newSource:       func(device string) (capture.Source, error) { return capture.NewMicSource(device) },
		systemSupported: capture.SystemAudioSupported,
		newSystemSource: func() (capture.Source, error) {
			src, err := capture.NewSystemSource()
			if err != nil {
				return nil, err // never a typed-nil Source
			}
			return src, nil
		},
		newBatchEngine: newBatchEngine,
		startSonioxCleanup: func() {
			if cfg.Soniox.APIKey == "" {
				return
			}
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), sonioxCleanupTimeout)
				defer cancel()
				_ = stt.NewSonioxAsyncEngine(cfg.Soniox.APIKey, "").CleanupStale(ctx)
			}()
		},
		newTranslator:  buildTranslator,
		newSummarizer:  summarize.New,
		pipelines:      map[string]*pipeline.Pipeline{},
		newCloudClient: nil,
	}
	surface.SetSelf(func() meeting.Self {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.cfg == nil {
			return meeting.Self{}
		}
		p := s.cfg.Profile
		return meeting.Self{Name: p.Name, Emails: p.Emails, Aliases: p.Aliases}
	})
	s.mcpHTTP = mcpserver.NewHTTPServer(surface, "")
	if cfg != nil {
		s.mcpHTTP.Apply(cfg.MCP)
	}
	s.startSonioxCleanup()
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /api/devices", s.handleDevices)
	s.mux.HandleFunc("GET /api/audio/sources", s.handleAudioSources)
	s.mux.HandleFunc("POST /api/meetings/{id}/source", s.handleSetMeetingSource)
	s.mux.HandleFunc("GET /api/meetings/{id}/levels", s.handleMeetingLevels)
	s.mux.HandleFunc("GET /api/meetings", s.handleListMeetings)
	s.mux.HandleFunc("POST /api/meetings", s.handleStartMeeting)
	s.mux.HandleFunc("GET /api/meetings/{id}", s.handleGetMeeting)
	s.mux.HandleFunc("PATCH /api/meetings/{id}", s.handleUpdateMeeting)
	s.mux.HandleFunc("DELETE /api/meetings/{id}", s.handleDeleteMeeting)
	s.mux.HandleFunc("GET /api/meetings/{id}/transcript", s.handleTranscript)
	s.mux.HandleFunc("GET /api/meetings/{id}/export", s.handleExport)
	s.mux.HandleFunc("GET /api/meetings/{id}/analysis", s.handleMeetingAnalysis)
	s.mux.HandleFunc("POST /api/meetings/{id}/flows", s.handleFindFlows)
	s.mux.HandleFunc("POST /api/meetings/{id}/notes", s.handleAddNote)
	s.mux.HandleFunc("POST /api/meetings/{id}/end", s.handleEndMeeting)
	s.mux.HandleFunc("POST /api/meetings/{id}/refine", s.handleRefine)
	s.mux.HandleFunc("POST /api/meetings/import", s.handleImportMeeting)
	s.mux.HandleFunc("GET /api/action-items", s.handleListActionItems)
	s.mux.HandleFunc("GET /api/projects", s.handleListProjects)
	s.mux.HandleFunc("POST /api/projects/rename", s.handleRenameProject)
	s.mux.HandleFunc("PATCH /api/meetings/{id}/action-items/{index}", s.handleSetActionItemStatus)
	s.mux.HandleFunc("GET /api/meetings/{id}/speakers", s.handleMeetingSpeakers)
	s.mux.HandleFunc("POST /api/guests/parse", s.handleParseGuests)
	s.mux.HandleFunc("GET /api/config", s.handleGetConfig)
	s.mux.HandleFunc("PUT /api/config", s.handlePutConfig)
	s.mux.HandleFunc("GET /api/translate/status", s.handleTranslateStatus)
	s.mux.HandleFunc("POST /api/translate/pull", s.handleTranslatePull)
	s.mux.HandleFunc("POST /api/translate/test", s.handleTranslateTest)
	s.mux.HandleFunc("GET /api/stt/status", s.handleSttStatus)
	s.mux.HandleFunc("POST /api/providers/{name}/verify", s.handleVerifyProvider)
	s.mux.HandleFunc("GET /api/mcp", s.handleGetMCP)
	s.mux.HandleFunc("PUT /api/mcp", s.handlePutMCP)
	s.mux.HandleFunc("POST /api/mcp/token", s.handleMCPToken)
	s.mux.HandleFunc("GET /api/system", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, sysinfo.ReadMachine())
	})
	s.mux.HandleFunc("GET /api/summary/status", s.handleSummaryStatus)

	s.mux.HandleFunc("POST /api/cloud/signup", s.handleCloudSignup)
	s.mux.HandleFunc("POST /api/cloud/login", s.handleCloudLogin)
	s.mux.HandleFunc("POST /api/cloud/logout", s.handleCloudLogout)
	s.mux.HandleFunc("GET /api/cloud/status", s.handleCloudStatus)
	s.mux.HandleFunc("POST /api/cloud/sync", s.handleCloudSync)
	s.mux.HandleFunc("POST /api/cloud/recover", s.handleCloudRecover)
	s.mux.HandleFunc("POST /api/cloud/change-password", s.handleCloudChangePassword)
	s.mux.HandleFunc("PUT /api/cloud/server", s.handleCloudServer)
	s.mux.HandleFunc("GET /api/cloud/sessions", s.handleCloudSessions)
	s.mux.HandleFunc("DELETE /api/cloud/sessions/{id}", s.handleCloudRevokeSession)
	s.mux.HandleFunc("POST /api/cloud/sessions/revoke-others", s.handleCloudRevokeOtherSessions)
	s.mux.HandleFunc("POST /api/cloud/totp/setup", s.handleCloudTOTPSetup)
	s.mux.HandleFunc("POST /api/cloud/totp/enable", s.handleCloudTOTPEnable)
	s.mux.HandleFunc("POST /api/cloud/totp/disable", s.handleCloudTOTPDisable)

	// Serves the embedded web UI for anything not matched above (with SPA
	// fallback); see webui.go. Registered last for readability only — Go's
	// ServeMux always prefers the most specific pattern regardless of
	// registration order, so this can never shadow /healthz or /api/*.
	s.mux.Handle("/", webUIHandler())
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleListMeetings(w http.ResponseWriter, r *http.Request) {
	if v := r.URL.Query().Get("q"); v != "" {
		ms, err := s.surface.SearchMeetings(v)
		respond(w, ms, err)
		return
	}
	ms, err := s.surface.ListMeetings()
	respond(w, ms, err)
}

func (s *Server) handleListActionItems(w http.ResponseWriter, _ *http.Request) {
	items, err := s.surface.ListActionItems()
	respond(w, items, err)
}

// handleListProjects serves the projects derived from the meetings'
// project tags: [{name, meetings, last_meeting_at}], most recent first.
func (s *Server) handleListProjects(w http.ResponseWriter, _ *http.Request) {
	ps, err := s.surface.ListProjects()
	respond(w, ps, err)
}

// handleRenameProject moves every meeting of project "from" to "to"
// (body {"from", "to"}); renaming onto an existing project merges them.
// Answers {"renamed": n}.
func (s *Server) handleRenameProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	n, err := s.surface.RenameProject(req.From, req.To)
	if errors.Is(err, tools.ErrProjectName) {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	respond(w, map[string]int{"renamed": n}, err)
}

func (s *Server) handleDevices(w http.ResponseWriter, _ *http.Request) {
	devices, err := capture.Devices()
	if err != nil {
		// Degrade gracefully: an environment with no usable audio backend
		// still gets a working API, just with no devices to pick from.
		devices = nil
	}
	writeJSON(w, http.StatusOK, devices)
}

// handleAudioSources serves the M19 source-selection contract: the mic
// device list (same shape as /api/devices, which stays) plus system
// capability.
func (s *Server) handleAudioSources(w http.ResponseWriter, _ *http.Request) {
	mics, err := capture.Devices()
	if err != nil {
		// Degrade gracefully like handleDevices: no audio backend still
		// gets a working answer.
		mics = nil
	}
	supported, reason := s.systemSupported()
	writeJSON(w, http.StatusOK, map[string]any{
		"mic_devices": mics,
		"system":      map[string]any{"supported": supported, "reason": reason},
	})
}

// handleSetMeetingSource switches a live meeting's audio source
// in place: the pipeline's mixer swaps inputs (open new first; on
// failure the old input keeps running). 404 when the meeting has no
// live pipeline (already ended or capture never started), 400 on a bad
// source value, 422 when the new source can't be opened (old input
// stays live; the body says why).
func (s *Server) handleSetMeetingSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	p := s.pipelines[id]
	s.mu.Unlock()
	if p == nil {
		httpErrorCode(w, http.StatusNotFound, "no_live_pipeline", "no live pipeline for this meeting")
		return
	}

	var req struct {
		Source string `json:"source"`
		Device string `json:"device"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	mode := SourceMic
	if req.Source != "" {
		mode = SourceMode(req.Source)
	}
	if !mode.valid() {
		httpError(w, http.StatusBadRequest, `source must be "mic", "system" or "both"`)
		return
	}

	if captureErr, captureKind := s.switchAudioSource(p, mode, req.Device); captureErr != "" {
		// Old input still live; the switch just didn't happen.
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error":              captureErr,
			"capture_error_kind": string(captureKind),
		})
		return
	}

	// Record the switch in the meeting so the timeline is honest:
	// audio_source frontmatter plus a timestamped note (notes are the
	// existing free-form timeline surface).
	if err := s.surface.Store().UpdateDoc(id, func(m *meeting.Meeting) error {
		m.AudioSource = string(mode)
		if m.Notes == nil {
			m.Notes = []string{}
		}
		m.Notes = append(m.Notes, fmt.Sprintf("audio source switched to %s at %s", mode, time.Now().Format(time.RFC3339)))
		return nil
	}); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"source": string(mode), "device": req.Device})
}

// handleMeetingLevels serves the live level meters (5–10 Hz polling):
// per active input rms/peak (0–1) plus drop diagnostics — dropped_ticks
// (output-side, slow consumer) and per-input dropped_samples (clock
// drift trims). Memory only; no disk I/O.
func (s *Server) handleMeetingLevels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	p := s.pipelines[id]
	s.mu.Unlock()
	if p == nil {
		httpErrorCode(w, http.StatusNotFound, "no_live_pipeline", "no live pipeline for this meeting")
		return
	}
	mixer := p.Mixer()
	if mixer == nil {
		httpErrorCode(w, http.StatusNotFound, "no_live_pipeline", "no live pipeline for this meeting")
		return
	}

	mic, sys, droppedTicks := mixer.Stats()
	body := map[string]any{"dropped_ticks": droppedTicks}
	if mixer.HasInput(true) {
		body["mic"] = map[string]any{
			"rms": mic.RMS, "peak": mic.Peak,
			"dropped_chunks": mic.DroppedChunks, "dropped_samples": mic.DroppedSamples,
		}
	}
	if mixer.HasInput(false) {
		sysBody := map[string]any{
			"rms": sys.RMS, "peak": sys.Peak,
			"dropped_chunks": sys.DroppedChunks, "dropped_samples": sys.DroppedSamples,
		}
		// M25: only sent when true (macOS, nothing but zeros since the
		// tap started — usually a denied System Audio Recording grant).
		if sys.NoSignal {
			sysBody["no_signal"] = true
		}
		body["system"] = sysBody
	}
	if state, errMsg, ok := p.EngineState(); ok {
		engineBody := map[string]any{"state": string(state)}
		if errMsg != "" {
			engineBody["error"] = errMsg
		}
		body["engine"] = engineBody
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleStartMeeting(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string   `json:"title"`
		Device      string   `json:"device"`
		Engine      string   `json:"engine"`
		Source      string   `json:"source"`
		Lang        string   `json:"lang"`
		TranslateTo string   `json:"translate_to"`
		TwoWay      bool     `json:"two_way"`
		Tags        []string `json:"tags"`
		// Project, when set, is added as the meeting's project tag.
		Project string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if req.Title == "" {
		httpError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.TwoWay && (req.Lang == "" || req.TranslateTo == "") {
		httpError(w, http.StatusBadRequest, "two_way requires both lang and translate_to (language A and B)")
		return
	}
	tags := req.Tags
	if meeting.NormalizeProject(req.Project) != "" {
		project, err := tools.ValidProjectName(req.Project)
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		tags = meeting.WithProject(tags, project)
	}
	m, err := s.surface.StartMeetingWithOptions(req.Title, meeting.StartOptions{
		Engine:      req.Engine,
		Lang:        req.Lang,
		TranslateTo: req.TranslateTo,
		TwoWay:      req.TwoWay,
		Tags:        tags,
	})
	if err != nil {
		respond(w, nil, err)
		return
	}
	mode := SourceMic
	if req.Source != "" {
		mode = SourceMode(req.Source)
		if !mode.valid() {
			httpError(w, http.StatusBadRequest, `source must be "mic", "system" or "both"`)
			return
		}
	}
	var captureErr, micErr string
	var captureKind CaptureErrorKind
	if req.Engine != "" {
		// Best-effort: a meeting always gets created even if live capture
		// can't start (e.g. no audio backend, or the engine can't connect).
		captureErr, captureKind, micErr = s.startPipeline(m.ID, req.Device, req.Engine, stt.Options{
			Lang:        req.Lang,
			TranslateTo: req.TranslateTo,
			LanguageB:   req.TranslateTo,
			TwoWay:      req.TwoWay,
		}, mode)
	}
	if err := s.surface.Store().UpdateDoc(m.ID, func(mm *meeting.Meeting) error {
		mm.AudioSource = string(mode)
		return nil
	}); err == nil {
		m.AudioSource = string(mode)
	}

	// Response shape: the Meeting object itself (as before), with
	// audio_source set, plus capture_error ONLY when capture failed to
	// start — the M20 contract keeps the meeting fields at top level.
	body := meetingJSON(m)
	if captureErr != "" {
		body["capture_error"] = captureErr
		body["capture_error_kind"] = captureKind
	}
	// "both" without a working mic: recording computer audio only.
	if micErr != "" {
		body["mic_error"] = micErr
	}
	writeJSON(w, http.StatusOK, body)
}

// meetingJSON round-trips a Meeting through its JSON tags into a map so
// extra response fields (capture_error) can sit alongside it.
func meetingJSON(m *meeting.Meeting) map[string]any {
	raw, err := json.Marshal(m)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

// SourceMode selects a meeting's audio inputs (M19): mic, system, or
// both mixed into one stream.
type SourceMode string

const (
	SourceMic    SourceMode = "mic"
	SourceSystem SourceMode = "system"
	SourceBoth   SourceMode = "both"
)

func (m SourceMode) valid() bool {
	return m == SourceMic || m == SourceSystem || m == SourceBoth
}

// CaptureErrorKind classifies a capture start/switch failure for the
// UI: the "Open settings" button routes to the settings section that
// can actually fix the cause, switching on this enum instead of
// pattern-matching the human-readable message (whose wording changes
// with the OS/driver and must stay free to reword). The message field
// (capture_error) stays the display text.
//
// Kinds are a stable wire contract (web/src/lib/api.ts mirrors them);
// add values only — never rename or remove.
type CaptureErrorKind string

const (
	// CaptureErrMissingAPIKey: the configured engine is missing its
	// credential (Soniox API key today).
	CaptureErrMissingAPIKey CaptureErrorKind = "missing_api_key"
	// CaptureErrDeviceUnavailable: an audio input couldn't be opened —
	// missing/disconnected/busy mic, or no usable system-audio device.
	CaptureErrDeviceUnavailable CaptureErrorKind = "device_unavailable"
	// CaptureErrUnsupported: this platform can't do what was asked at
	// all (system audio on Linux/macOS).
	CaptureErrUnsupported CaptureErrorKind = "unsupported"
	// CaptureErrEngine: the engine exists but couldn't start/reach its
	// service, or the engine name itself is unknown.
	CaptureErrEngine CaptureErrorKind = "engine"
	// CaptureErrInternal: pipeline construction/mixer failure with no
	// more specific classification.
	CaptureErrInternal CaptureErrorKind = "internal"
)

// startPipeline runs the meeting's capture through a Mixer for EVERY
// mode (a one-input mixer is still a mixer: one code path, and
// mid-session switching never needs to rebuild the pipeline). Returns
// a human-readable capture error plus its kind on failure — the meeting
// still exists (existing behavior), but the failure is no longer
// silent.
//
// micErr is set (and captureErr empty) when "both" couldn't open the
// microphone but computer audio started: the meeting records computer
// audio only rather than nothing (a Mac mini has no built-in mic).
func (s *Server) startPipeline(id, device, engineName string, opts stt.Options, mode SourceMode) (captureErr string, kind CaptureErrorKind, micErr string) {
	engine, err := s.newEngine(engineName, s.cfg)
	if err != nil {
		if isMissingKey(err) {
			return err.Error(), CaptureErrMissingAPIKey, ""
		}
		return err.Error(), CaptureErrEngine, ""
	}

	var micSrc, sysSrc capture.Source
	if mode == SourceMic || mode == SourceBoth {
		micSrc, err = s.newSource(device)
		if err != nil {
			micErr = "Couldn't open microphone: " + err.Error()
			if ok, _ := s.systemSupported(); mode == SourceMic || !ok {
				return micErr, CaptureErrDeviceUnavailable, ""
			}
			micSrc = nil // "both": carry on with computer audio alone
		}
	}
	if mode == SourceSystem || mode == SourceBoth {
		if ok, reason := s.systemSupported(); !ok {
			if micSrc != nil {
				_ = micSrc.Close()
			}
			return reason, CaptureErrUnsupported, ""
		}
		sysSrc, err = s.newSystemSource()
		if err != nil {
			if micSrc != nil {
				_ = micSrc.Close()
			}
			if micErr != "" { // neither input opened: report the mic, as before
				return micErr, CaptureErrDeviceUnavailable, ""
			}
			return err.Error(), CaptureErrDeviceUnavailable, ""
		}
	}

	src, err := capture.NewMixer(capture.RealClock{}, micSrc, sysSrc)
	if err != nil {
		if micSrc != nil {
			_ = micSrc.Close()
		}
		if sysSrc != nil {
			_ = sysSrc.Close()
		}
		return err.Error(), CaptureErrInternal, ""
	}

	var translator translate.Translator
	if opts.TwoWay || opts.TranslateTo != "" {
		// Best-effort: a translator that fails to construct just leaves
		// the pipeline running untranslated rather than blocking capture.
		if t, err := s.newTranslator(s.cfg, s.cfg.Translate.LiveModel); err == nil {
			translator = t
		}
	}
	audioPath := filepath.Join(s.surface.Store().Root(), id, "audio.wav")
	p, err := pipeline.Start(s.surface.Store(), id, src, engine, opts, audioPath, translator)
	if err != nil {
		_ = src.Close()
		if micSrc != nil {
			_ = micSrc.Close()
		}
		if sysSrc != nil {
			_ = sysSrc.Close()
		}
		return err.Error(), CaptureErrEngine, ""
	}
	s.mu.Lock()
	s.pipelines[id] = p
	s.mu.Unlock()
	return "", "", micErr
}

// switchAudioSource swaps a live pipeline's inputs through the mixer:
// open the new sources FIRST; only on success SetInputs and close the
// old; on failure keep the old input and return the error. audio.wav
// keeps recording the mixed stream throughout (the mixer's output
// channel is never touched).
func (s *Server) switchAudioSource(p *pipeline.Pipeline, mode SourceMode, device string) (captureErr string, kind CaptureErrorKind) {
	mixer := p.Mixer()
	if mixer == nil {
		return "pipeline has no mixer", CaptureErrInternal
	}

	var micSrc, sysSrc capture.Source
	var err error
	if mode == SourceMic || mode == SourceBoth {
		micSrc, err = s.newSource(device)
		if err != nil {
			return "Couldn't open microphone: " + err.Error(), CaptureErrDeviceUnavailable
		}
	}
	if mode == SourceSystem || mode == SourceBoth {
		if ok, reason := s.systemSupported(); !ok {
			if micSrc != nil {
				_ = micSrc.Close()
			}
			return reason, CaptureErrUnsupported
		}
		sysSrc, err = s.newSystemSource()
		if err != nil {
			if micSrc != nil {
				_ = micSrc.Close()
			}
			return err.Error(), CaptureErrDeviceUnavailable
		}
	}

	// Commit: swap inputs; the mixer hands back the displaced sources,
	// which we close (ownership transfer is explicit).
	displaced, err := mixer.SetInputs(micSrc, sysSrc)
	if err != nil {
		// The mixer closed between the pipeline check and here (meeting
		// ended mid-switch): nothing was installed, so close the newly
		// opened sources and report.
		if micSrc != nil {
			_ = micSrc.Close()
		}
		if sysSrc != nil {
			_ = sysSrc.Close()
		}
		return "meeting ended during switch", CaptureErrInternal
	}
	for _, old := range displaced {
		_ = old.Close()
	}
	return "", ""
}

func (s *Server) stopPipeline(id string) {
	s.mu.Lock()
	p := s.pipelines[id]
	delete(s.pipelines, id)
	s.mu.Unlock()
	if p != nil {
		_ = p.Stop()
	}
}

// newEngine builds an stt.StreamEngine by name, falling back to cfg's
// configured default when name is empty.
// buildTranslator is the production newTranslator. model is the Ollama
// model the caller wants (Translate.LiveModel for live meetings,
// BatchModel for re-transcribe/import); cloud providers ignore it except
// that the LLM uses its strong model for batch work and its fast one
// live. Every caller treats an error as "no text translator", so an
// unconfigured provider just leaves lines untranslated (the readiness
// checks in /api/translate/status tell the user why).
func buildTranslator(cfg *config.Config, model string) (translate.Translator, error) {
	switch cfg.Translate.Provider {
	case config.TranslateNone:
		return nil, errTranslateNone
	case config.TranslateGoogle:
		if cfg.Translate.Google.APIKey == "" {
			return nil, errors.New("Google Translate API key is not set — add it in Settings → Translation")
		}
		return &translate.GoogleTranslator{APIKey: cfg.Translate.Google.APIKey}, nil
	case config.TranslateLLM:
		base, fast, strong := cfg.LLM.Resolved()
		if !cfg.LLM.Ready() {
			return nil, errors.New("the AI model provider isn't set up — add its API key in Settings → Translation")
		}
		m := fast
		if model == cfg.Translate.BatchModel && model != cfg.Translate.LiveModel {
			m = strong
		}
		return &translate.LLMTranslator{BaseURL: base, APIKey: cfg.LLM.APIKey, Model: m}, nil
	default:
		return &translate.OllamaTranslator{Host: cfg.Translate.Host, Model: model}, nil
	}
}

// errTranslateNone: translate.provider is "none", so there's no text
// translator — only an engine's native translation.
var errTranslateNone = errors.New("no translation provider is set up: only the transcription engine's own translation is available")

func newEngine(name string, cfg *config.Config) (stt.StreamEngine, error) {
	if name == "" {
		name = cfg.Engine
	}
	if name == "" || name == "whisper" {
		return stt.NewWhisperEngine(cfg.WhisperURL), nil
	}
	return cloudStream(name, cfg)
}

func (s *Server) handleGetMeeting(w http.ResponseWriter, r *http.Request) {
	m, err := s.surface.GetMeeting(r.PathValue("id"))
	respond(w, m, err)
}

func (s *Server) handleTranscript(w http.ResponseWriter, r *http.Request) {
	cursor, err := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
	if err != nil {
		cursor = -1
	}
	id := r.PathValue("id")
	// source: "live" (default, cursor-paged for the Live view), "v2" (the
	// batch transcript a refine wrote), or "best" (v2 when there is one,
	// else live). Meeting pages pick the transcript an analysis's
	// evidence seqs refer to (analysis.generated.transcript).
	source := r.URL.Query().Get("source")
	switch source {
	case "", "live":
		source = "live"
	case "v2", "best":
		batch, err := s.surface.ReadBatchTranscript(id)
		if err != nil {
			respond(w, nil, err)
			return
		}
		if source == "v2" || len(batch) > 0 {
			// The batch transcript is small and written whole: page it
			// with the same cursor contract (seqs after cursor).
			out := []meeting.Utterance{}
			next := cursor
			for _, u := range batch {
				if u.Seq > cursor {
					out = append(out, u)
					next = u.Seq
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"utterances": out, "cursor": next, "source": "v2"})
			return
		}
		source = "live"
	default:
		httpError(w, http.StatusBadRequest, `source must be "live", "v2" or "best"`)
		return
	}
	us, next, err := s.surface.ReadLiveTranscript(id, cursor)
	if err != nil {
		respond(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"utterances": us, "cursor": next, "source": source})
}

func (s *Server) handleAddNote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if req.Note == "" {
		httpError(w, http.StatusBadRequest, "note is required")
		return
	}
	err := s.surface.AddNote(r.PathValue("id"), req.Note)
	respond(w, map[string]string{"status": "ok"}, err)
}

func (s *Server) handleEndMeeting(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.surface.EndMeeting(id)
	s.stopPipeline(id)
	respond(w, map[string]string{"status": "ok"}, err)
}

// handleUpdateMeeting patches a meeting's title and/or tags (body
// {"title"?: string, "tags"?: string[]}; at least one must be present).
func (s *Server) handleUpdateMeeting(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title *string   `json:"title"`
		Tags  *[]string `json:"tags"`
		// Speakers merges confirmed names (an empty name removes one);
		// Guests replaces the guest list.
		Speakers map[string]string `json:"speakers"`
		Guests   *[]meeting.Guest  `json:"guests"`
		// Me is the key of the speaker who is the user; "" forgets it.
		Me *string `json:"me"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if req.Title == nil && req.Tags == nil && req.Speakers == nil && req.Guests == nil && req.Me == nil {
		httpError(w, http.StatusBadRequest, "title, tags, speakers, guests or me is required")
		return
	}
	if req.Title != nil && *req.Title == "" {
		httpError(w, http.StatusBadRequest, "title is required")
		return
	}
	id := r.PathValue("id")
	if req.Title != nil {
		if err := s.surface.RenameMeeting(id, *req.Title); err != nil {
			respondErr(w, err)
			return
		}
	}
	if req.Tags != nil {
		if err := s.surface.SetMeetingTags(id, *req.Tags); err != nil {
			respondErr(w, err)
			return
		}
	}
	if req.Speakers != nil {
		if err := s.surface.SetSpeakerNames(id, req.Speakers); err != nil {
			respondInputErr(w, err)
			return
		}
	}
	if req.Guests != nil {
		if err := s.surface.SetGuests(id, *req.Guests); err != nil {
			respondInputErr(w, err)
			return
		}
	}
	// After speakers: picking "me" drops that speaker's typed name.
	if req.Me != nil {
		if err := s.surface.SetMeSpeaker(id, *req.Me); err != nil {
			respondInputErr(w, err)
			return
		}
	}
	m, err := s.surface.GetMeeting(id)
	respond(w, m, err)
}

// respondInputErr answers a store error, or a 400 for a value the store
// refused (too long, too many).
func respondInputErr(w http.ResponseWriter, err error) {
	if errors.Is(err, meeting.ErrNotFound) {
		respondErr(w, err)
		return
	}
	httpError(w, http.StatusBadRequest, err.Error())
}

// handleMeetingSpeakers lists a transcript's speakers (?source= as for
// the transcript: best by default) with their confirmed names, plus the
// user's own name and the meeting's guests: what naming needs.
func (s *Server) handleMeetingSpeakers(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m, err := s.surface.GetMeeting(id)
	if err != nil {
		respondErr(w, err)
		return
	}
	var us []meeting.Utterance
	source := r.URL.Query().Get("source")
	if source != "live" {
		if us, err = s.surface.ReadBatchTranscript(id); err != nil {
			respondErr(w, err)
			return
		}
	}
	if source == "live" || (source != "v2" && len(us) == 0) {
		if us, _, err = s.surface.ReadLiveTranscript(id, -1); err != nil {
			respondErr(w, err)
			return
		}
	}
	type speaker struct {
		meeting.SpeakerStat
		Name string `json:"name,omitempty"` // confirmed only
	}
	out := []speaker{}
	for _, st := range meeting.SpeakerStats(us) {
		out = append(out, speaker{SpeakerStat: st, Name: m.Speakers[st.Key]})
	}
	guests := m.Guests
	if guests == nil {
		guests = []meeting.Guest{}
	}
	namer := s.surface.Namer(m).WithTranscript(us)
	writeJSON(w, http.StatusOK, map[string]any{
		"speakers": out, "guests": guests,
		"me":      namer.Me,     // the user's name
		"me_key":  m.Me,         // the speaker they said is them
		"auto_me": namer.AutoMe, // the microphone's guess when they haven't
	})
}

// handleParseGuests reads a pasted guest list for the UI to review
// before saving it with PATCH /api/meetings/{id}.
func (s *Server) handleParseGuests(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	guests := meeting.ParseGuests(req.Text)
	if guests == nil {
		guests = []meeting.Guest{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"guests": guests})
}

func (s *Server) handleDeleteMeeting(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m, err := s.surface.GetMeeting(id)
	if err != nil {
		respondErr(w, err)
		return
	}
	if m.EndedAt == nil {
		// Mirrors handleRefine's active-meeting conflict: deleting a
		// meeting that is still being captured would orphan its pipeline.
		httpErrorCode(w, http.StatusConflict, "meeting_active", "meeting is still active")
		return
	}
	s.stopPipeline(id)
	if err := s.surface.DeleteMeeting(id); err != nil {
		respondErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSetActionItemStatus updates one action item's status in place.
// Unknown meeting and out-of-range index are both 404s but with distinct
// error messages so the two causes stay debuggable.
func (s *Server) handleSetActionItemStatus(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "action item index must be a number")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if req.Status == "" {
		httpError(w, http.StatusBadRequest, "status is required")
		return
	}
	id := r.PathValue("id")
	if err := s.surface.Store().SetActionItemStatus(id, index, req.Status); err != nil {
		if errors.Is(err, meeting.ErrActionItemIndex) {
			httpErrorCode(w, http.StatusNotFound, "action_item_not_found", err.Error())
			return
		}
		respondErr(w, err)
		return
	}
	m, err := s.surface.GetMeeting(id)
	respond(w, m, err)
}

// respondSummaryErr answers a classified summary failure as
// {error, kind} (plus reset_at for quota_exceeded); anything else is a
// plain store error.
func respondSummaryErr(w http.ResponseWriter, err error) {
	var se *summarize.Error
	if !errors.As(err, &se) {
		respondErr(w, err)
		return
	}
	body := map[string]string{"error": se.Message, "kind": string(se.Kind)}
	status := http.StatusBadGateway
	switch se.Kind {
	case summarize.KindConsentRequired:
		status = http.StatusConflict
	case summarize.KindQuotaExceeded:
		status = http.StatusTooManyRequests
		if se.ResetAt != "" {
			body["reset_at"] = se.ResetAt
		}
	case summarize.KindNotReady:
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, body)
}

// translateStatusTimeout bounds how long a status check waits on an
// unreachable Ollama before reporting it as such, so the request doesn't
// hang for the default HTTP client timeout.
const translateStatusTimeout = 5 * time.Second

// resolveTranslateModel maps a "live"/"batch"/"summary" target to the
// configured Ollama model name.
func (s *Server) resolveTranslateModel(target string) (string, bool) {
	switch target {
	case "live":
		return s.cfg.Translate.LiveModel, true
	case "batch":
		return s.cfg.Translate.BatchModel, true
	case "summary":
		return s.cfg.Summary.OllamaModel, true
	default:
		return "", false
	}
}

func (s *Server) handleTranslateStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), translateStatusTimeout)
	defer cancel()

	s.mu.Lock()
	cfg := *s.cfg
	s.mu.Unlock()
	// Cloud providers and "none": no Ollama probe. ready says whether
	// text translation works; reason says what's missing.
	switch cfg.Translate.Provider {
	case config.TranslateNone:
		writeJSON(w, http.StatusOK, map[string]any{"provider": cfg.Translate.Provider, "ready": false,
			"reason": "Only the transcription engine's own translation (Soniox) is used."})
		return
	case config.TranslateGoogle:
		body := map[string]any{"provider": cfg.Translate.Provider, "ready": cfg.Translate.Google.APIKey != ""}
		if cfg.Translate.Google.APIKey == "" {
			body["reason"] = "Add your Google Cloud Translation API key."
		}
		writeJSON(w, http.StatusOK, body)
		return
	case config.TranslateLLM:
		body := map[string]any{"provider": cfg.Translate.Provider, "ready": cfg.LLM.Ready()}
		if !cfg.LLM.Ready() {
			body["reason"] = "Set up your AI provider (API key and model)."
		}
		writeJSON(w, http.StatusOK, body)
		return
	}

	names, err := translate.ListModels(ctx, s.cfg.Translate.Host)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"provider":         config.TranslateOllama,
			"ready":            false,
			"ollama_reachable": false,
			"live_model":       map[string]any{"name": s.cfg.Translate.LiveModel, "installed": false},
			"batch_model":      map[string]any{"name": s.cfg.Translate.BatchModel, "installed": false},
		})
		return
	}
	installed := make(map[string]bool, len(names))
	for _, n := range names {
		installed[n] = true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"provider":         config.TranslateOllama,
		"ready":            installed[s.cfg.Translate.LiveModel],
		"ollama_reachable": true,
		"live_model":       map[string]any{"name": s.cfg.Translate.LiveModel, "installed": installed[s.cfg.Translate.LiveModel]},
		"batch_model":      map[string]any{"name": s.cfg.Translate.BatchModel, "installed": installed[s.cfg.Translate.BatchModel]},
	})
}

func (s *Server) handleTranslatePull(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	model, ok := s.resolveTranslateModel(req.Target)
	if !ok {
		httpError(w, http.StatusBadRequest, `target must be "live", "batch" or "summary"`)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)

	err := translate.Pull(r.Context(), s.cfg.Translate.Host, model, func(status string) {
		_ = enc.Encode(map[string]string{"status": status})
		if flusher != nil {
			flusher.Flush()
		}
	})
	if err != nil {
		_ = enc.Encode(map[string]string{"error": err.Error()})
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func (s *Server) handleTranslateTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text       string `json:"text"`
		TargetLang string `json:"target_lang"`
		Target     string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if req.Text == "" || req.TargetLang == "" {
		httpError(w, http.StatusBadRequest, "text and target_lang are required")
		return
	}
	if req.Target == "" {
		req.Target = "live"
	}
	model, ok := s.resolveTranslateModel(req.Target)
	if !ok {
		httpError(w, http.StatusBadRequest, `target must be "live" or "batch"`)
		return
	}

	translator, err := s.newTranslator(s.cfg, model)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	translated, err := translator.Translate(r.Context(), req.Text, req.TargetLang)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"translated": translated})
}

// sttStatusTimeout bounds the Whisper reachability probe so the status
// check answers quickly when nothing is listening.
const sttStatusTimeout = 1500 * time.Millisecond

// sttEngineStatus is one engine's readiness in GET /api/stt/status.
// Reason is plain, user-facing text, set only when Ready is false.
type sttEngineStatus struct {
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"`
}

// handleSttStatus reports whether each transcription engine is ready
// before a meeting starts. It always answers 200: an unreachable Whisper
// server is a status, not a server error. Soniox is never dialled; a set
// API key is the only check.
func (s *Server) handleSttStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := *s.cfg
	s.mu.Unlock()

	defaultEngine := "whisper"
	if _, ok := findCloudEngine(cfg.Engine); ok {
		defaultEngine = cfg.Engine
	}

	ctx, cancel := context.WithTimeout(r.Context(), sttStatusTimeout)
	defer cancel()
	whisper := sttEngineStatus{Ready: true}
	if err := stt.WhisperReachable(ctx, cfg.WhisperURL); err != nil {
		whisper = sttEngineStatus{Reason: fmt.Sprintf(
			"The Whisper server at %s isn't responding. Start it, or switch to the cloud engine.", cfg.WhisperURL)}
	}
	body := map[string]any{
		"default_engine": defaultEngine,
		"whisper":        whisper,
	}
	for _, e := range cloudEngines {
		st := sttEngineStatus{Ready: true}
		if e.key(&cfg) == "" {
			st = sttEngineStatus{Reason: fmt.Sprintf("Add a %s API key in Settings → Transcription.", e.label)}
		}
		body[e.name] = st
	}
	writeJSON(w, http.StatusOK, body)
}

// sanitizedConfig renders cfg in the JSON shape shared by GET and PUT
// /api/config: the same field names as Config's YAML, except the Soniox
// and summary OpenAI API keys are replaced by top-level
// soniox_api_key_set / summary_openai_api_key_set booleans so the
// secrets themselves never appear in any response.
func sanitizedConfig(cfg *config.Config) map[string]any {
	return map[string]any{
		"setup":                    cfg.Setup,
		"engine":                   cfg.Engine,
		"lang":                     cfg.Lang,
		"ui_language":              cfg.UILanguage,
		"whisper_url":              cfg.WhisperURL,
		"soniox":                   map[string]any{"url": cfg.Soniox.URL},
		"soniox_api_key_set":       cfg.Soniox.APIKey != "",
		"deepgram_api_key_set":     cfg.Deepgram.APIKey != "",
		"elevenlabs_api_key_set":   cfg.ElevenLabs.APIKey != "",
		"speechmatics_api_key_set": cfg.Speechmatics.APIKey != "",
		"gladia_api_key_set":       cfg.Gladia.APIKey != "",
		"translate": map[string]any{
			"provider":           cfg.Translate.Provider,
			"google_api_key_set": cfg.Translate.Google.APIKey != "",
			"host":               cfg.Translate.Host,
			"live_model":         cfg.Translate.LiveModel,
			"batch_model":        cfg.Translate.BatchModel,
		},
		"llm":             llmJSON(cfg.LLM),
		"llm_api_key_set": cfg.LLM.APIKey != "",
		"profile":         profileJSON(cfg.Profile),
		"summary": map[string]any{
			"provider":      summarize.ProviderOf(cfg),
			"ollama_model":  cfg.Summary.OllamaModel,
			"cloud_consent": cfg.Summary.CloudConsent,
		},
	}
}

// llmJSON is the shared LLM block as the UI sees it: what the user set
// plus the resolved server and models (preset defaults filled in), so
// the form can show placeholders. Never the key.
func llmJSON(l config.LLMConfig) map[string]any {
	base, fast, strong := l.Resolved()
	return map[string]any{
		"preset":       l.Preset,
		"base_url":     l.BaseURL,
		"account_id":   l.AccountID,
		"fast_model":   l.FastModel,
		"strong_model": l.StrongModel,
		"resolved": map[string]any{
			"base_url":     base,
			"fast_model":   fast,
			"strong_model": strong,
		},
	}
}

// configRequestBody mirrors sanitizedConfig's shape plus the write-only
// soniox_api_key and summary.openai.api_key fields, which never appear
// in responses.
type configRequestBody struct {
	Setup      string `json:"setup"`
	Engine     string `json:"engine"`
	Lang       string `json:"lang"`
	UILanguage string `json:"ui_language"`
	WhisperURL string `json:"whisper_url"`
	Soniox     struct {
		URL string `json:"url"`
	} `json:"soniox"`
	SonioxAPIKey       string `json:"soniox_api_key"`
	DeepgramAPIKey     string `json:"deepgram_api_key"`
	ElevenLabsAPIKey   string `json:"elevenlabs_api_key"`
	SpeechmaticsAPIKey string `json:"speechmatics_api_key"`
	GladiaAPIKey       string `json:"gladia_api_key"`
	Translate          struct {
		Provider     string `json:"provider"`
		GoogleAPIKey string `json:"google_api_key"`
		Host         string `json:"host"`
		LiveModel    string `json:"live_model"`
		BatchModel   string `json:"batch_model"`
	} `json:"translate"`
	// LLM fields are pointers: "" must be settable (back to the preset's
	// default model, clearing a custom URL), and omitted means as-is.
	LLM struct {
		Preset      *string `json:"preset"`
		BaseURL     *string `json:"base_url"`
		AccountID   *string `json:"account_id"`
		FastModel   *string `json:"fast_model"`
		StrongModel *string `json:"strong_model"`
		APIKey      string  `json:"api_key"`
	} `json:"llm"`
	// Profile.Name is a pointer so it can be cleared.
	// Emails and Aliases replace the lists when present.
	Profile struct {
		Name    *string   `json:"name"`
		Emails  *[]string `json:"emails"`
		Aliases *[]string `json:"aliases"`
	} `json:"profile"`
	Summary struct {
		Provider    string `json:"provider"`
		OllamaModel string `json:"ollama_model"`
		// A pointer, unlike the strings: false (revoking consent) must
		// be settable, and omitted must still mean "leave as-is".
		CloudConsent *bool `json:"cloud_consent"`
	} `json:"summary"`
}

// Config reads and writes are guarded by s.mu (the same mutex that
// already guards s.pipelines): a PUT swaps the pointer while concurrent
// requests may be reading it.
func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	body := sanitizedConfig(s.cfg)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, body)
}

// setIfNonEmpty overwrites *dst with v unless v is empty, so an omitted
// (or explicitly blank) request field leaves the existing value alone.
func setIfNonEmpty(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var req configRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if req.Engine != "" && !validEngine(req.Engine) {
		httpError(w, http.StatusBadRequest, "engine: unknown engine "+strconv.Quote(req.Engine))
		return
	}
	switch req.Summary.Provider {
	case "", summarize.ProviderOllama, summarize.ProviderCloud, summarize.ProviderOpenAI:
	default:
		httpError(w, http.StatusBadRequest, `summary.provider must be "ollama", "catchm-cloud" or "openai"`)
		return
	}
	switch req.Setup {
	case "", config.SetupCloud, config.SetupSelfHost, config.SetupCustom:
	default:
		httpError(w, http.StatusBadRequest, `setup must be "cloud", "selfhost" or "custom"`)
		return
	}
	switch req.Translate.Provider {
	case "", config.TranslateOllama, config.TranslateGoogle, config.TranslateLLM, config.TranslateNone:
	default:
		httpError(w, http.StatusBadRequest, `translate.provider must be "ollama", "google", "llm" or "none"`)
		return
	}
	if p := req.LLM.Preset; p != nil && *p != config.LLMCustom {
		if _, ok := config.LLMPresets[*p]; !ok {
			httpError(w, http.StatusBadRequest, "llm.preset: unknown preset "+strconv.Quote(*p))
			return
		}
	}
	var llmBaseURL string
	if req.LLM.BaseURL != nil && *req.LLM.BaseURL != "" {
		normalized, err := validateSummaryBaseURL(*req.LLM.BaseURL)
		if err != nil {
			httpError(w, http.StatusBadRequest, "llm.base_url: "+err.Error())
			return
		}
		llmBaseURL = normalized
	}
	switch req.UILanguage {
	case "", "system", "en", "vi":
	default:
		httpError(w, http.StatusBadRequest, `ui_language must be "system", "en" or "vi"`)
		return
	}
	if n := req.Profile.Name; n != nil {
		*n = strings.Join(strings.Fields(*n), " ")
		if utf8.RuneCountInString(*n) > config.MaxProfileName {
			httpError(w, http.StatusBadRequest, fmt.Sprintf("profile.name: at most %d characters", config.MaxProfileName))
			return
		}
	}
	if e := req.Profile.Emails; e != nil {
		clean, err := cleanProfileEmails(*e)
		if err != nil {
			httpError(w, http.StatusBadRequest, "profile.emails: "+err.Error())
			return
		}
		*e = clean
	}
	if a := req.Profile.Aliases; a != nil {
		clean, err := cleanProfileAliases(*a)
		if err != nil {
			httpError(w, http.StatusBadRequest, "profile.aliases: "+err.Error())
			return
		}
		*a = clean
	}

	s.mu.Lock()
	updated := *s.cfg // shallow copy is enough: the profile lists below are replaced, never edited in place
	s.mu.Unlock()
	// A field left empty in the request means "leave it as-is", not
	// "clear it" — a partial PUT (e.g. {"engine":"soniox"} alone) must
	// not blank out the rest of the config on disk.
	setIfNonEmpty(&updated.Setup, req.Setup)
	setIfNonEmpty(&updated.Engine, req.Engine)
	setIfNonEmpty(&updated.Lang, req.Lang)
	setIfNonEmpty(&updated.UILanguage, req.UILanguage)
	setIfNonEmpty(&updated.WhisperURL, req.WhisperURL)
	setIfNonEmpty(&updated.Soniox.URL, req.Soniox.URL)
	setIfNonEmpty(&updated.Soniox.APIKey, req.SonioxAPIKey)
	setIfNonEmpty(&updated.Deepgram.APIKey, req.DeepgramAPIKey)
	setIfNonEmpty(&updated.ElevenLabs.APIKey, req.ElevenLabsAPIKey)
	setIfNonEmpty(&updated.Speechmatics.APIKey, req.SpeechmaticsAPIKey)
	setIfNonEmpty(&updated.Gladia.APIKey, req.GladiaAPIKey)
	setIfNonEmpty(&updated.Translate.Provider, req.Translate.Provider)
	setIfNonEmpty(&updated.Translate.Google.APIKey, req.Translate.GoogleAPIKey)
	setIfNonEmpty(&updated.Translate.Host, req.Translate.Host)
	setIfNonEmpty(&updated.Translate.LiveModel, req.Translate.LiveModel)
	setIfNonEmpty(&updated.Translate.BatchModel, req.Translate.BatchModel)
	setIfNonEmpty(&updated.Summary.Provider, req.Summary.Provider)
	setIfNonEmpty(&updated.Summary.OllamaModel, req.Summary.OllamaModel)
	applyLLMUpdate(&updated.LLM, req, llmBaseURL)
	if req.Profile.Name != nil {
		updated.Profile.Name = *req.Profile.Name
	}
	if req.Profile.Emails != nil {
		updated.Profile.Emails = *req.Profile.Emails
	}
	if req.Profile.Aliases != nil {
		updated.Profile.Aliases = *req.Profile.Aliases
	}
	updated.Normalize()
	if req.Summary.CloudConsent != nil {
		updated.Summary.CloudConsent = *req.Summary.CloudConsent
	}

	if err := config.Save(&updated); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	reloaded, err := config.Load()
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.mu.Lock()
	s.cfg = reloaded
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, sanitizedConfig(reloaded))
}

// applyLLMUpdate merges a PUT's llm fields. A stored key belongs to the
// server it was entered for: when the request moves the LLM to another
// server (preset, custom URL or Cloudflare account) without sending a
// new key, the old key is dropped rather than sent somewhere new.
func applyLLMUpdate(l *config.LLMConfig, req configRequestBody, baseURL string) {
	beforeBase, _, _ := l.Resolved()
	if req.LLM.Preset != nil {
		l.Preset = *req.LLM.Preset
	}
	if req.LLM.BaseURL != nil {
		l.BaseURL = baseURL
	}
	if req.LLM.AccountID != nil {
		l.AccountID = strings.TrimSpace(*req.LLM.AccountID)
	}
	if req.LLM.FastModel != nil {
		l.FastModel = strings.TrimSpace(*req.LLM.FastModel)
	}
	if req.LLM.StrongModel != nil {
		l.StrongModel = strings.TrimSpace(*req.LLM.StrongModel)
	}
	afterBase, _, _ := l.Resolved()
	if req.LLM.APIKey != "" {
		l.APIKey = req.LLM.APIKey
	} else if beforeBase != afterBase {
		l.APIKey = ""
	}
}

func respondRefineErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, meeting.ErrNotFound):
		httpErrorCode(w, http.StatusNotFound, "meeting_not_found", err.Error())
	case errors.Is(err, batch.ErrMeetingActive):
		httpErrorCode(w, http.StatusConflict, "meeting_active", err.Error())
	case errors.Is(err, batch.ErrNoAudio):
		httpErrorCode(w, http.StatusConflict, "no_audio", err.Error())
	default:
		httpError(w, http.StatusInternalServerError, err.Error())
	}
}

func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func respondErr(w http.ResponseWriter, err error) {
	if errors.Is(err, meeting.ErrNotFound) {
		httpErrorCode(w, http.StatusNotFound, "meeting_not_found", err.Error())
		return
	}
	httpError(w, http.StatusInternalServerError, err.Error())
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// httpErrorCode is httpError plus a stable machine-readable code (msg
// stays English for logs, MCP and agents; the UI keys off the code).
func httpErrorCode(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

// profileJSON is the profile as the UI sees it (lists never null).
func profileJSON(p config.ProfileConfig) map[string]any {
	emails, aliases := p.Emails, p.Aliases
	if emails == nil {
		emails = []string{}
	}
	if aliases == nil {
		aliases = []string{}
	}
	return map[string]any{"name": p.Name, "emails": emails, "aliases": aliases}
}

// cleanProfileEmails lowercases, deduplicates and checks the user's
// emails.
func cleanProfileEmails(in []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, e := range in {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || seen[e] {
			continue
		}
		if a, err := mail.ParseAddress(e); err != nil || a.Address != e {
			return nil, fmt.Errorf("%q isn't an email address", e)
		}
		seen[e] = true
		out = append(out, e)
	}
	if len(out) > config.MaxProfileEntries {
		return nil, fmt.Errorf("at most %d", config.MaxProfileEntries)
	}
	return out, nil
}

// cleanProfileAliases trims and deduplicates the names people call the
// user by.
func cleanProfileAliases(in []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, a := range in {
		a = strings.Join(strings.Fields(a), " ")
		if a == "" || seen[strings.ToLower(a)] {
			continue
		}
		if utf8.RuneCountInString(a) > config.MaxProfileName {
			return nil, fmt.Errorf("a name has at most %d characters", config.MaxProfileName)
		}
		seen[strings.ToLower(a)] = true
		out = append(out, a)
	}
	if len(out) > config.MaxProfileEntries {
		return nil, fmt.Errorf("at most %d", config.MaxProfileEntries)
	}
	return out, nil
}
