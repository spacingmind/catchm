package mcpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/tools"
)

// ErrDisabled: MCP is off in Settings, so `catchm mcp` won't serve.
var ErrDisabled = errors.New("the CatchM MCP server is turned off: turn it on in CatchM → Settings → MCP")

// RunStdio serves MCP over stdin/stdout until the client hangs up — the
// `catchm mcp` / `catchm-desktop mcp` subcommand an MCP client launches.
// It reads the meeting folder directly, so the app needn't be running.
func RunStdio(ctx context.Context, version string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.MCP.Enabled {
		return ErrDisabled
	}
	home, err := config.HomeDir()
	if err != nil {
		return err
	}
	store, err := meeting.NewStore(filepath.Join(home, "meetings"))
	if err != nil {
		return err
	}
	return New(tools.New(store), Options{AllowWrites: cfg.MCP.AllowWrites, Version: version}).Run(ctx, &mcp.StdioTransport{})
}

// NewToken returns a random bearer token for the HTTP transport.
func NewToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// HTTPStatus is the local HTTP transport's state for the settings UI.
type HTTPStatus struct {
	Running bool   `json:"running"`
	URL     string `json:"url,omitempty"`
	Error   string `json:"error,omitempty"`
}

// HTTPServer runs the Streamable HTTP transport on 127.0.0.1 while the
// app is open. Apply (re)starts or stops it to match the config.
type HTTPServer struct {
	surface *tools.Surface
	version string

	mu     sync.Mutex
	srv    *http.Server
	status HTTPStatus
}

func NewHTTPServer(surface *tools.Surface, version string) *HTTPServer {
	return &HTTPServer{surface: surface, version: version}
}

func (h *HTTPServer) Status() HTTPStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status
}

// Apply stops any running listener, then starts one when MCP and its
// HTTP transport are both on (and a token is set).
func (h *HTTPServer) Apply(c config.MCPConfig) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = h.srv.Shutdown(ctx)
		cancel()
		h.srv = nil
	}
	h.status = HTTPStatus{}
	if !c.Enabled || !c.HTTP.Enabled {
		return
	}
	if c.HTTP.Token == "" {
		h.status.Error = "no token set"
		return
	}
	port := c.HTTP.Port
	if port == 0 {
		port = config.DefaultMCPPort
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		h.status.Error = fmt.Sprintf("couldn't open port %d: %v", port, err)
		return
	}
	server := New(h.surface, Options{AllowWrites: c.AllowWrites, Version: h.version})
	// The SDK adds DNS-rebinding protection (Host/Origin must be
	// loopback) for localhost listeners; the token keeps other local
	// programs and web pages out.
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", requireToken(c.HTTP.Token, mcpHandler))
	h.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	h.status = HTTPStatus{Running: true, URL: "http://" + addr + "/mcp"}
	go func(srv *http.Server) {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.mu.Lock()
			if h.srv == srv {
				h.status = HTTPStatus{Error: err.Error()}
			}
			h.mu.Unlock()
		}
	}(h.srv)
}

// Close stops the listener (app shutdown).
func (h *HTTPServer) Close() { h.Apply(config.MCPConfig{}) }

func requireToken(token string, next http.Handler) http.Handler {
	want := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != want {
			w.Header().Set("WWW-Authenticate", `Bearer realm="catchm"`)
			http.Error(w, "missing or wrong token: copy it from CatchM → Settings → MCP", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
