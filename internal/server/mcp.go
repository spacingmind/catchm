package server

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/mcpserver"
)

// mcpJSON is GET/PUT /api/mcp: the switches, the HTTP transport's live
// status (with its token — the local settings UI needs it to set up a
// client) and the stdio command an MCP client should run.
func (s *Server) mcpJSON(c config.MCPConfig) map[string]any {
	port := c.HTTP.Port
	if port == 0 {
		port = config.DefaultMCPPort
	}
	exe, _ := os.Executable()
	st := s.mcpHTTP.Status()
	return map[string]any{
		"enabled":      c.Enabled,
		"allow_writes": c.AllowWrites,
		"http": map[string]any{
			"enabled": c.HTTP.Enabled,
			"port":    port,
			"token":   c.HTTP.Token,
			"running": st.Running,
			"url":     st.URL,
			"error":   st.Error,
		},
		"stdio": map[string]any{"command": exe, "args": []string{"mcp"}},
	}
}

func (s *Server) handleGetMCP(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	c := s.cfg.MCP
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, s.mcpJSON(c))
}

// handlePutMCP takes a partial update; omitted fields are left as-is.
// Turning HTTP on without a token makes one.
func (s *Server) handlePutMCP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled     *bool `json:"enabled"`
		AllowWrites *bool `json:"allow_writes"`
		HTTPEnabled *bool `json:"http_enabled"`
		Port        *int  `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if req.Port != nil && (*req.Port < 1024 || *req.Port > 65535) {
		httpError(w, http.StatusBadRequest, "port must be between 1024 and 65535")
		return
	}
	s.updateMCP(w, func(c *config.MCPConfig) {
		if req.Enabled != nil {
			c.Enabled = *req.Enabled
		}
		if req.AllowWrites != nil {
			c.AllowWrites = *req.AllowWrites
		}
		if req.HTTPEnabled != nil {
			c.HTTP.Enabled = *req.HTTPEnabled
		}
		if req.Port != nil {
			c.HTTP.Port = *req.Port
		}
		if c.HTTP.Enabled && c.HTTP.Token == "" {
			c.HTTP.Token = mcpserver.NewToken()
		}
	})
}

// handleMCPToken replaces the HTTP token; clients using the old one stop
// working.
func (s *Server) handleMCPToken(w http.ResponseWriter, _ *http.Request) {
	s.updateMCP(w, func(c *config.MCPConfig) { c.HTTP.Token = mcpserver.NewToken() })
}

func (s *Server) updateMCP(w http.ResponseWriter, fn func(*config.MCPConfig)) {
	s.mu.Lock()
	updated := *s.cfg
	s.mu.Unlock()
	fn(&updated.MCP)
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
	s.mcpHTTP.Apply(reloaded.MCP)
	writeJSON(w, http.StatusOK, s.mcpJSON(reloaded.MCP))
}
