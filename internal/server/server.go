// Package server exposes the meeting tool surface over HTTP and serves
// the embedded web UI.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/spacingmind/catchm/internal/agent"
	"github.com/spacingmind/catchm/internal/batch"
	"github.com/spacingmind/catchm/internal/capture"
	"github.com/spacingmind/catchm/internal/cloudsync"
	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/pipeline"
	"github.com/spacingmind/catchm/internal/stt"
	"github.com/spacingmind/catchm/internal/tools"
	"github.com/spacingmind/catchm/internal/translate"
)

// Server serves the CatchM HTTP API.
type Server struct {
	surface *tools.Surface
	cfg     *config.Config
	mux     *http.ServeMux

	newEngine      func(name string, cfg *config.Config) (stt.StreamEngine, error)
	newSource      func(device string) (capture.Source, error)
	newBatchEngine func(cfg *config.Config) (stt.BatchEngine, error)
	newTranslator  func(cfg *config.Config, model string) (translate.Translator, error)
	enrichFinal    func(ctx context.Context, store *meeting.Store, id string) error

	mu        sync.Mutex
	pipelines map[string]*pipeline.Pipeline

	// cloud is the in-memory-only cloud-sync session; nil when logged
	// out. See cloud.go.
	cloud   *cloudState
	syncNow func() (*cloudsync.SyncResult, error)
	// newCloudClient is a seam for tests to swap in a fake backend.
	newCloudClient func() *cloudsync.Client
}

// New builds a Server over the tool surface. cfg may be nil, in which case
// engine defaults apply as if no config file were present.
func New(surface *tools.Surface, cfg *config.Config) *Server {
	if cfg == nil {
		cfg = &config.Config{}
	}
	s := &Server{
		surface:        surface,
		cfg:            cfg,
		mux:            http.NewServeMux(),
		newEngine:      newEngine,
		newSource:      func(device string) (capture.Source, error) { return capture.NewMicSource(device) },
		newBatchEngine: func(cfg *config.Config) (stt.BatchEngine, error) { return stt.NewWhisperEngine(cfg.WhisperURL), nil },
		newTranslator: func(cfg *config.Config, model string) (translate.Translator, error) {
			return &translate.OllamaTranslator{Host: cfg.Translate.Host, Model: model}, nil
		},
		enrichFinal:    agent.EnrichMeetingFinal,
		pipelines:      map[string]*pipeline.Pipeline{},
		newCloudClient: nil,
	}
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
	s.mux.HandleFunc("POST /api/meetings/{id}/notes", s.handleAddNote)
	s.mux.HandleFunc("POST /api/meetings/{id}/end", s.handleEndMeeting)
	s.mux.HandleFunc("POST /api/meetings/{id}/refine", s.handleRefine)
	s.mux.HandleFunc("GET /api/action-items", s.handleListActionItems)
	s.mux.HandleFunc("PATCH /api/meetings/{id}/action-items/{index}", s.handleSetActionItemStatus)
	s.mux.HandleFunc("GET /api/config", s.handleGetConfig)
	s.mux.HandleFunc("PUT /api/config", s.handlePutConfig)
	s.mux.HandleFunc("GET /api/translate/status", s.handleTranslateStatus)
	s.mux.HandleFunc("POST /api/translate/pull", s.handleTranslatePull)
	s.mux.HandleFunc("POST /api/translate/test", s.handleTranslateTest)

	s.mux.HandleFunc("POST /api/cloud/signup", s.handleCloudSignup)
	s.mux.HandleFunc("POST /api/cloud/login", s.handleCloudLogin)
	s.mux.HandleFunc("POST /api/cloud/logout", s.handleCloudLogout)
	s.mux.HandleFunc("GET /api/cloud/status", s.handleCloudStatus)
	s.mux.HandleFunc("POST /api/cloud/sync", s.handleCloudSync)
	s.mux.HandleFunc("POST /api/cloud/recover", s.handleCloudRecover)
	s.mux.HandleFunc("PUT /api/cloud/server", s.handleCloudServer)

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
	supported, reason := capture.SystemAudioSupported()
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
		httpError(w, http.StatusNotFound, "no live pipeline for this meeting")
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

	if captureErr := s.switchAudioSource(p, mode, req.Device); captureErr != "" {
		// Old input still live; the switch just didn't happen.
		httpError(w, http.StatusUnprocessableEntity, captureErr)
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
		httpError(w, http.StatusNotFound, "no live pipeline for this meeting")
		return
	}
	mixer := p.Mixer()
	if mixer == nil {
		httpError(w, http.StatusNotFound, "no live pipeline for this meeting")
		return
	}

	mic, sys, droppedTicks := mixer.Stats()
	body := map[string]any{"dropped_ticks": droppedTicks}
	if mic.RMS != 0 || mic.Peak != 0 || micActive(mixer) {
		body["mic"] = map[string]any{"rms": mic.RMS, "peak": mic.Peak, "dropped_samples": mic.DroppedSamples}
	}
	if sys.RMS != 0 || sys.Peak != 0 || sysActive(mixer) {
		body["system"] = map[string]any{"rms": sys.RMS, "peak": sys.Peak, "dropped_samples": sys.DroppedSamples}
	}
	writeJSON(w, http.StatusOK, body)
}

func micActive(m *capture.Mixer) bool { return sourceActive(m, true) }
func sysActive(m *capture.Mixer) bool { return sourceActive(m, false) }
func sourceActive(m *capture.Mixer, mic bool) bool {
	// Active = the mixer has that input wired (regardless of momentary
	// level); the mixer exposes this via Stats inputs presence below.
	return m.HasInput(mic)
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
	m, err := s.surface.StartMeetingWithOptions(req.Title, meeting.StartOptions{
		Engine:      req.Engine,
		Lang:        req.Lang,
		TranslateTo: req.TranslateTo,
		TwoWay:      req.TwoWay,
		Tags:        req.Tags,
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
	var captureErr string
	if req.Engine != "" {
		// Best-effort: a meeting always gets created even if live capture
		// can't start (e.g. no audio backend, or the engine can't connect).
		captureErr = s.startPipeline(m.ID, req.Device, req.Engine, stt.Options{
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

// startPipeline runs the meeting's capture through a Mixer for EVERY
// mode (a one-input mixer is still a mixer: one code path, and
// mid-session switching never needs to rebuild the pipeline). Returns
// a human-readable capture error on failure — the meeting still exists
// (existing behavior), but the failure is no longer silent.
func (s *Server) startPipeline(id, device, engineName string, opts stt.Options, mode SourceMode) (captureErr string) {
	engine, err := s.newEngine(engineName, s.cfg)
	if err != nil {
		return err.Error()
	}

	var micSrc, sysSrc capture.Source
	if mode == SourceMic || mode == SourceBoth {
		micSrc, err = s.newSource(device)
		if err != nil {
			return "Couldn't open microphone: " + err.Error()
		}
	}
	if mode == SourceSystem || mode == SourceBoth {
		if ok, reason := capture.SystemAudioSupported(); !ok {
			if micSrc != nil {
				_ = micSrc.Close()
			}
			return reason
		}
		sysSrc, err = capture.NewSystemSource()
		if err != nil {
			if micSrc != nil {
				_ = micSrc.Close()
			}
			return err.Error()
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
		return err.Error()
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
		return err.Error()
	}
	s.mu.Lock()
	s.pipelines[id] = p
	s.mu.Unlock()
	return ""
}

// switchAudioSource swaps a live pipeline's inputs through the mixer:
// open the new sources FIRST; only on success SetInputs and close the
// old; on failure keep the old input and return the error. audio.wav
// keeps recording the mixed stream throughout (the mixer's output
// channel is never touched).
func (s *Server) switchAudioSource(p *pipeline.Pipeline, mode SourceMode, device string) (captureErr string) {
	mixer := p.Mixer()
	if mixer == nil {
		return "pipeline has no mixer"
	}

	var micSrc, sysSrc capture.Source
	var err error
	if mode == SourceMic || mode == SourceBoth {
		micSrc, err = s.newSource(device)
		if err != nil {
			return "Couldn't open microphone: " + err.Error()
		}
	}
	if mode == SourceSystem || mode == SourceBoth {
		if ok, reason := capture.SystemAudioSupported(); !ok {
			if micSrc != nil {
				_ = micSrc.Close()
			}
			return reason
		}
		sysSrc, err = capture.NewSystemSource()
		if err != nil {
			if micSrc != nil {
				_ = micSrc.Close()
			}
			return err.Error()
		}
	}

	// Commit: swap inputs, then close the superseded sources.
	mixer.SetInputs(micSrc, sysSrc)
	if micSrc == nil {
		if old := s.takeOldMic(p); old != nil {
			_ = old.Close()
		}
	}
	return ""
}

// takeOldMic is a no-op placeholder — old sources are closed via the
// mixer swap registry (SetInputs stops their pumps; the pipeline
// closes them at Stop). Kept for clarity of the failure protocol.
func (s *Server) takeOldMic(p *pipeline.Pipeline) capture.Source { return nil }

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
func newEngine(name string, cfg *config.Config) (stt.StreamEngine, error) {
	if name == "" {
		name = cfg.Engine
	}
	switch name {
	case "", "whisper":
		return stt.NewWhisperEngine(cfg.WhisperURL), nil
	case "soniox":
		if cfg.Soniox.APIKey == "" {
			return nil, errors.New("soniox: no api key configured")
		}
		return stt.NewSonioxEngine(cfg.Soniox.URL, cfg.Soniox.APIKey), nil
	default:
		return nil, fmt.Errorf("unknown stt engine %q", name)
	}
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
	us, next, err := s.surface.ReadLiveTranscript(r.PathValue("id"), cursor)
	if err != nil {
		respond(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"utterances": us, "cursor": next})
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if req.Title == nil && req.Tags == nil {
		httpError(w, http.StatusBadRequest, "title or tags is required")
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
	m, err := s.surface.GetMeeting(id)
	respond(w, m, err)
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
		httpError(w, http.StatusConflict, "meeting is still active")
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
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		respondErr(w, err)
		return
	}
	m, err := s.surface.GetMeeting(id)
	respond(w, m, err)
}

func (s *Server) handleRefine(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	engine, err := s.newBatchEngine(s.cfg)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var translator translate.Translator
	if m, err := s.surface.GetMeeting(id); err == nil && m.TranslateTo != "" {
		if t, err := s.newTranslator(s.cfg, s.cfg.Translate.BatchModel); err == nil {
			translator = t
		}
	}
	if err := batch.Refine(r.Context(), s.surface.Store(), id, engine, translator); err != nil {
		respondRefineErr(w, err)
		return
	}
	if err := s.enrichFinal(r.Context(), s.surface.Store(), id); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	m, err := s.surface.GetMeeting(id)
	respond(w, m, err)
}

// translateStatusTimeout bounds how long a status check waits on an
// unreachable Ollama before reporting it as such, so the request doesn't
// hang for the default HTTP client timeout.
const translateStatusTimeout = 5 * time.Second

// resolveTranslateModel maps a "live"/"batch" target to the configured
// model name.
func (s *Server) resolveTranslateModel(target string) (string, bool) {
	switch target {
	case "live":
		return s.cfg.Translate.LiveModel, true
	case "batch":
		return s.cfg.Translate.BatchModel, true
	default:
		return "", false
	}
}

func (s *Server) handleTranslateStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), translateStatusTimeout)
	defer cancel()

	names, err := translate.ListModels(ctx, s.cfg.Translate.Host)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
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
		httpError(w, http.StatusBadRequest, `target must be "live" or "batch"`)
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

// sanitizedConfig renders cfg in the JSON shape shared by GET and PUT
// /api/config: the same field names as Config's YAML, except the Soniox
// API key is replaced by a top-level soniox_api_key_set boolean so the
// secret itself never appears in any response.
func sanitizedConfig(cfg *config.Config) map[string]any {
	return map[string]any{
		"engine":             cfg.Engine,
		"lang":               cfg.Lang,
		"whisper_url":        cfg.WhisperURL,
		"soniox":             map[string]any{"url": cfg.Soniox.URL},
		"soniox_api_key_set": cfg.Soniox.APIKey != "",
		"translate": map[string]any{
			"host":        cfg.Translate.Host,
			"live_model":  cfg.Translate.LiveModel,
			"batch_model": cfg.Translate.BatchModel,
		},
	}
}

// configRequestBody mirrors sanitizedConfig's shape plus the write-only
// soniox_api_key field, which never appears in responses.
type configRequestBody struct {
	Engine     string `json:"engine"`
	Lang       string `json:"lang"`
	WhisperURL string `json:"whisper_url"`
	Soniox     struct {
		URL string `json:"url"`
	} `json:"soniox"`
	SonioxAPIKey string `json:"soniox_api_key"`
	Translate    struct {
		Host       string `json:"host"`
		LiveModel  string `json:"live_model"`
		BatchModel string `json:"batch_model"`
	} `json:"translate"`
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
	if req.Engine != "" && req.Engine != "whisper" && req.Engine != "soniox" {
		httpError(w, http.StatusBadRequest, `engine must be "whisper" or "soniox"`)
		return
	}

	s.mu.Lock()
	updated := *s.cfg // shallow copy is enough: Config holds no reference types
	s.mu.Unlock()
	// A field left empty in the request means "leave it as-is", not
	// "clear it" — a partial PUT (e.g. {"engine":"soniox"} alone) must
	// not blank out the rest of the config on disk.
	setIfNonEmpty(&updated.Engine, req.Engine)
	setIfNonEmpty(&updated.Lang, req.Lang)
	setIfNonEmpty(&updated.WhisperURL, req.WhisperURL)
	setIfNonEmpty(&updated.Soniox.URL, req.Soniox.URL)
	setIfNonEmpty(&updated.Soniox.APIKey, req.SonioxAPIKey)
	setIfNonEmpty(&updated.Translate.Host, req.Translate.Host)
	setIfNonEmpty(&updated.Translate.LiveModel, req.Translate.LiveModel)
	setIfNonEmpty(&updated.Translate.BatchModel, req.Translate.BatchModel)

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

func respondRefineErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, meeting.ErrNotFound):
		httpError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, batch.ErrMeetingActive), errors.Is(err, batch.ErrNoAudio):
		httpError(w, http.StatusConflict, err.Error())
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
		httpError(w, http.StatusNotFound, err.Error())
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
