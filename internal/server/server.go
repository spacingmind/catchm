// Package server exposes the meeting tool surface over HTTP and serves
// the embedded web UI.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/spacingmind/catchm/internal/capture"
	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/pipeline"
	"github.com/spacingmind/catchm/internal/stt"
	"github.com/spacingmind/catchm/internal/tools"
)

// Server serves the CatchM HTTP API.
type Server struct {
	surface *tools.Surface
	cfg     *config.Config
	mux     *http.ServeMux

	newEngine func(name string, cfg *config.Config) (stt.StreamEngine, error)
	newSource func(device string) (capture.Source, error)

	mu        sync.Mutex
	pipelines map[string]*pipeline.Pipeline
}

// New builds a Server over the tool surface. cfg may be nil, in which case
// engine defaults apply as if no config file were present.
func New(surface *tools.Surface, cfg *config.Config) *Server {
	if cfg == nil {
		cfg = &config.Config{}
	}
	s := &Server{
		surface:   surface,
		cfg:       cfg,
		mux:       http.NewServeMux(),
		newEngine: newEngine,
		newSource: func(device string) (capture.Source, error) { return capture.NewMicSource(device) },
		pipelines: map[string]*pipeline.Pipeline{},
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /api/devices", s.handleDevices)
	s.mux.HandleFunc("GET /api/meetings", s.handleListMeetings)
	s.mux.HandleFunc("POST /api/meetings", s.handleStartMeeting)
	s.mux.HandleFunc("GET /api/meetings/{id}", s.handleGetMeeting)
	s.mux.HandleFunc("GET /api/meetings/{id}/transcript", s.handleTranscript)
	s.mux.HandleFunc("POST /api/meetings/{id}/notes", s.handleAddNote)
	s.mux.HandleFunc("POST /api/meetings/{id}/end", s.handleEndMeeting)
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

func (s *Server) handleDevices(w http.ResponseWriter, _ *http.Request) {
	devices, err := capture.Devices()
	if err != nil {
		// Degrade gracefully: an environment with no usable audio backend
		// still gets a working API, just with no devices to pick from.
		devices = nil
	}
	writeJSON(w, http.StatusOK, devices)
}

func (s *Server) handleStartMeeting(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string `json:"title"`
		Device      string `json:"device"`
		Engine      string `json:"engine"`
		Lang        string `json:"lang"`
		TranslateTo string `json:"translate_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if req.Title == "" {
		httpError(w, http.StatusBadRequest, "title is required")
		return
	}
	m, err := s.surface.StartMeetingWithOptions(req.Title, meeting.StartOptions{
		Engine:      req.Engine,
		Lang:        req.Lang,
		TranslateTo: req.TranslateTo,
	})
	if err != nil {
		respond(w, nil, err)
		return
	}
	if req.Engine != "" {
		// Best-effort: a meeting always gets created even if live capture
		// can't start (e.g. no audio backend, or the engine can't connect).
		s.startPipeline(m.ID, req.Device, req.Engine, req.Lang, req.TranslateTo)
	}
	respond(w, m, nil)
}

func (s *Server) startPipeline(id, device, engineName, lang, translateTo string) {
	engine, err := s.newEngine(engineName, s.cfg)
	if err != nil {
		return
	}
	src, err := s.newSource(device)
	if err != nil {
		return
	}
	audioPath := filepath.Join(s.surface.Store().Root(), id, "audio.wav")
	p, err := pipeline.Start(s.surface.Store(), id, src, engine, stt.Options{Lang: lang, TranslateTo: translateTo}, audioPath)
	if err != nil {
		_ = src.Close()
		return
	}
	s.mu.Lock()
	s.pipelines[id] = p
	s.mu.Unlock()
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
