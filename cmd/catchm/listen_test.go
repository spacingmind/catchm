package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoopbackAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		defaultAddr:      true,
		"localhost:4649": true,
		"[::1]:4649":     true,
		":4649":          false, // every interface
		"0.0.0.0:4649":   false,
		"192.168.1.5:80": false,
	} {
		if got := isLoopbackAddr(addr); got != want {
			t.Errorf("isLoopbackAddr(%q) = %v", addr, got)
		}
	}
}

// DNS rebinding: a request that reached 127.0.0.1 under another
// site's hostname is refused; this computer's names pass, any port
// (the Vite dev proxy keeps its own port).
func TestLoopbackOnly(t *testing.T) {
	h := loopbackOnly(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for host, want := range map[string]int{
		"127.0.0.1:4649":     http.StatusNoContent,
		"localhost:5173":     http.StatusNoContent,
		"LOCALHOST":          http.StatusNoContent,
		"[::1]:4649":         http.StatusNoContent,
		"evil.example:4649":  http.StatusForbidden,
		"192.168.1.5:4649":   http.StatusForbidden,
		"localhost.evil.com": http.StatusForbidden,
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/meetings", nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("Host %q: %d, want %d", host, w.Code, want)
		}
	}
	// A non-loopback listen address was chosen on purpose: no Host check.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "my-server.lan"
	w := httptest.NewRecorder()
	guardFor("0.0.0.0:4649", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("explicit LAN address: %d", w.Code)
	}
}
