package stt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestWhisperEngineStatusAlwaysStreaming proves Whisper reports a
// constant, honest "streaming" state: it has no persistent connection
// and no chunk failure that gives up the stream (see EngineStatus's doc
// comment), so there's nothing else true to report, before or after any
// Stream call.
func TestWhisperEngineStatusAlwaysStreaming(t *testing.T) {
	e := NewWhisperEngine("http://127.0.0.1:0")
	state, errMsg := e.EngineStatus()
	if state != EngineStreaming || errMsg != "" {
		t.Fatalf("EngineStatus() = (%q, %q), want (%q, \"\")", state, errMsg, EngineStreaming)
	}
}

// TestSonioxEngineStatusStreamsAfterConnect proves the engine reports
// EngineStreaming as soon as Stream's dial + config handshake succeeds
// (before any audio is sent) — the only state transition observable
// through Pipeline/levels, since a failed connect returns an error from
// Stream itself instead of ever producing a Pipeline to query (see
// EngineState's doc comment for why "connecting" isn't reachable here).
func TestSonioxEngineStatusStreamsAfterConnect(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.ReadMessage() // config frame
		<-r.Context().Done()
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	engine := NewSonioxEngine(wsURL, "test-key")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pcmCh, resultCh, err := engine.Stream(ctx, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer close(pcmCh)
	_ = resultCh

	state, errMsg := engine.EngineStatus()
	if state != EngineStreaming || errMsg != "" {
		t.Fatalf("EngineStatus() after connect = (%q, %q), want (%q, \"\")", state, errMsg, EngineStreaming)
	}
}

// TestSonioxEngineStatusNormalEndNotError proves a normal end — closing
// pcmCh, which makes the engine send its end-of-audio frame, after
// which the server closes the connection the way the real Soniox server
// does once it's flushed its finals — is never reported as EngineError,
// even though the read that discovers the closed connection looks
// identical (a ReadMessage error) to an unexpected drop.
func TestSonioxEngineStatusNormalEndNotError(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil { // config frame
			return
		}
		// Wait for the end-of-audio empty binary frame, then close —
		// the real server's documented behavior after flushing finals.
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if mt == websocket.BinaryMessage && len(data) == 0 {
				return // closing here (via the deferred conn.Close) is the point
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	engine := NewSonioxEngine(wsURL, "test-key")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pcmCh, resultCh, err := engine.Stream(ctx, Options{})
	if err != nil {
		t.Fatal(err)
	}

	close(pcmCh) // triggers end-of-audio; server then closes the conn

	select {
	case _, ok := <-resultCh:
		if ok {
			t.Fatal("expected resultCh to close with no results")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the session to end")
	}

	state, errMsg := engine.EngineStatus()
	if state != EngineStreaming || errMsg != "" {
		t.Fatalf("EngineStatus() after a normal end = (%q, %q), want unchanged (%q, \"\") — the server's expected close after end-of-audio must not be reported as an error", state, errMsg, EngineStreaming)
	}
}

// TestSonioxEngineStatusUnexpectedDropIsError proves an abrupt
// connection loss WHILE still actively streaming (before end-of-audio
// was ever sent) is reported as EngineError with a non-empty message —
// the case operators actually need surfaced in the live view.
func TestSonioxEngineStatusUnexpectedDropIsError(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil { // config frame
			conn.Close()
			return
		}
		// Read one audio chunk, then vanish without a clean close
		// handshake — an unexpected mid-session drop.
		conn.ReadMessage()
		conn.Close()
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	engine := NewSonioxEngine(wsURL, "test-key")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pcmCh, resultCh, err := engine.Stream(ctx, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer close(pcmCh)

	select {
	case pcmCh <- PCM{1, 2, 3, 4}:
	case <-time.After(time.Second):
		t.Fatal("timed out sending pcm chunk")
	}

	select {
	case _, ok := <-resultCh:
		if ok {
			t.Fatal("expected resultCh to close with no results")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the dropped session to be noticed")
	}

	state, errMsg := engine.EngineStatus()
	if state != EngineError || errMsg == "" {
		t.Fatalf("EngineStatus() after an unexpected drop = (%q, %q), want (%q, <non-empty>)", state, errMsg, EngineError)
	}
}
