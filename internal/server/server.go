// Package server exposes the meeting tool surface over HTTP and serves
// the embedded web UI.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/tools"
)

// Server serves the CatchM HTTP API.
type Server struct {
	surface *tools.Surface
	mux     *http.ServeMux
}

// New builds a Server over the tool surface.
func New(surface *tools.Surface) *Server {
	s := &Server{surface: surface, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
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

func (s *Server) handleStartMeeting(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if req.Title == "" {
		httpError(w, http.StatusBadRequest, "title is required")
		return
	}
	m, err := s.surface.StartMeeting(req.Title)
	respond(w, m, err)
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
	err := s.surface.EndMeeting(r.PathValue("id"))
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
