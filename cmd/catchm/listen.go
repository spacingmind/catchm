package main

import (
	"log"
	"net"
	"net/http"
	"strings"
)

// defaultAddr keeps the daemon on this computer. Its API has no login:
// anything that can reach it can read every meeting and change the
// config, so listening on all interfaces would hand that to the LAN.
const defaultAddr = "127.0.0.1:4649"

// isLoopbackAddr reports whether a listen address only accepts
// connections from this computer.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// isLoopbackHost reports whether a request's Host header names this
// computer ("localhost", "127.0.0.1", "[::1]", any port).
func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// loopbackOnly rejects requests whose Host isn't this computer. A page
// on another site can point its own hostname at 127.0.0.1 (DNS
// rebinding) and then talk to the daemon as same-origin; its requests
// still carry that hostname, so checking Host stops them.
func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// guardFor wraps the API for addr: on loopback (the default) it only
// answers requests addressed to this computer. A non-loopback address
// was asked for explicitly (CATCHM_ADDR); say what that exposes.
func guardFor(addr string, h http.Handler) http.Handler {
	if isLoopbackAddr(addr) {
		return loopbackOnly(h)
	}
	log.Printf("warning: CATCHM_ADDR=%s accepts connections from other computers; the API has no login, so they can read your meetings and change settings", addr)
	return h
}
