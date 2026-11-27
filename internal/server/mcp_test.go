package server

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestMCPEndpointsAndHTTP(t *testing.T) {
	t.Setenv("CATCHM_HOME", t.TempDir())
	s, _ := newTestServer(t)
	t.Cleanup(s.mcpHTTP.Close)

	type state struct {
		Enabled     bool `json:"enabled"`
		AllowWrites bool `json:"allow_writes"`
		HTTP        struct {
			Enabled bool   `json:"enabled"`
			Port    int    `json:"port"`
			Token   string `json:"token"`
			Running bool   `json:"running"`
			URL     string `json:"url"`
			Error   string `json:"error"`
		} `json:"http"`
		Stdio struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"stdio"`
	}
	var st state
	decode(t, do(t, s, "GET", "/api/mcp", ""), &st)
	if st.Enabled || st.HTTP.Running || st.Stdio.Command == "" || len(st.Stdio.Args) != 1 || st.Stdio.Args[0] != "mcp" {
		t.Fatalf("default = %+v", st)
	}

	port := freePort(t)
	body := `{"enabled":true,"http_enabled":true,"port":` + itoa(port) + `}`
	decode(t, do(t, s, "PUT", "/api/mcp", body), &st)
	if !st.Enabled || !st.HTTP.Running || st.HTTP.Token == "" || st.HTTP.Error != "" {
		t.Fatalf("after enable = %+v", st)
	}

	// No token → 401.
	resp, err := http.Post(st.HTTP.URL, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: %d", resp.StatusCode)
	}

	// With the token: read-only tools.
	listTools := func(token string) (int, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cl := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil)
		cs, err := cl.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: st.HTTP.URL, MaxRetries: -1, DisableStandaloneSSE: true,
			HTTPClient: &http.Client{Transport: bearer{token, http.DefaultTransport}}}, nil)
		if err != nil {
			return 0, err
		}
		defer cs.Close()
		res, err := cs.ListTools(ctx, nil)
		if err != nil {
			return 0, err
		}
		return len(res.Tools), nil
	}
	if n, err := listTools(st.HTTP.Token); err != nil || n != 6 {
		t.Fatalf("read-only tools: %d, %v", n, err)
	}
	decode(t, do(t, s, "PUT", "/api/mcp", `{"allow_writes":true}`), &st)
	if n, err := listTools(st.HTTP.Token); err != nil || n != 9 {
		t.Fatalf("with writes: %d, %v", n, err)
	}

	// A new token locks out the old one.
	old := st.HTTP.Token
	decode(t, do(t, s, "POST", "/api/mcp/token", ""), &st)
	if st.HTTP.Token == old {
		t.Fatal("token not replaced")
	}
	if _, err := listTools(old); err == nil {
		t.Fatal("old token still works")
	}

	// Off → port closed.
	decode(t, do(t, s, "PUT", "/api/mcp", `{"http_enabled":false}`), &st)
	if st.HTTP.Running {
		t.Fatalf("still running: %+v", st)
	}
	if c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", itoa(port)), time.Second); err == nil {
		c.Close()
		t.Fatal("port still open after turning HTTP off")
	}
	if w := do(t, s, "PUT", "/api/mcp", `{"port":80}`); w.Code != http.StatusBadRequest {
		t.Fatalf("port 80 accepted: %d", w.Code)
	}
}

func itoa(n int) string {
	b := []byte{}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
