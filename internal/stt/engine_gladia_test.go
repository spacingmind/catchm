package stt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestGladiaStream(t *testing.T) {
	var mu sync.Mutex
	var initBody map[string]any
	var stopped bool
	upgrader := websocket.Upgrader{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/live":
			if r.Header.Get("x-gladia-key") != "gl-key" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			mu.Lock()
			_ = json.NewDecoder(r.Body).Decode(&initBody)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"s1","url":"ws`+strings.TrimPrefix(srv.URL, "http")+`/ws?token=t"}`)
		case "/ws":
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			send := func(s string) { _ = conn.WriteMessage(websocket.TextMessage, []byte(s)) }
			first := true
			for {
				mt, data, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if mt == websocket.BinaryMessage && first {
					first = false
					send(`{"type":"transcript","data":{"id":"u1","is_final":false,"utterance":{"text":"Xin"}}}`)
					send(`{"type":"transcript","data":{"id":"u1","is_final":true,"utterance":{"text":" Xin chào mọi người. ","language":"vi"}}}`)
					send(`{"type":"speech_end","data":{}}`)
				}
				if mt == websocket.TextMessage && strings.Contains(string(data), "stop_recording") {
					mu.Lock()
					stopped = true
					mu.Unlock()
					send(`{"type":"transcript","data":{"id":"u2","is_final":true,"utterance":{"text":"Tạm biệt."}}}`)
					_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
					return
				}
			}
		}
	}))
	defer srv.Close()

	e := newGladiaEngine("gl-key", srv.URL)
	send, results, err := e.Stream(context.Background(), Options{Lang: "vi", ContextTerms: []string{"CatchM"}})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		send <- PCM(make([]byte, 640))
		time.Sleep(100 * time.Millisecond)
		close(send)
	}()
	var got []Result
	timeout := time.After(5 * time.Second)
loop:
	for {
		select {
		case r, ok := <-results:
			if !ok {
				break loop
			}
			got = append(got, r)
		case <-timeout:
			t.Fatalf("results never closed; got %+v", got)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	lc, _ := initBody["language_config"].(map[string]any)
	if initBody["encoding"] != "wav/pcm" || initBody["sample_rate"] != float64(16000) || lc["languages"].([]any)[0] != "vi" || initBody["realtime_processing"] == nil || !stopped {
		t.Errorf("init %v, stopped %v", initBody, stopped)
	}
	if len(got) != 2 || got[0].Text != "Xin chào mọi người." || got[1].Text != "Tạm biệt." || got[0].Speaker != "" {
		t.Fatalf("results = %+v", got)
	}
	if st, _ := e.EngineStatus(); st != EngineStreaming {
		t.Errorf("status after a clean end = %q", st)
	}
}

func TestGladiaBadKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"statusCode":401,"message":"gladia key not found"}`)
	}))
	defer srv.Close()
	e := newGladiaEngine("bad", srv.URL)
	if _, _, err := e.Stream(context.Background(), Options{}); err == nil || !strings.Contains(err.Error(), "rejected the API key") || !strings.Contains(err.Error(), "gladia key not found") {
		t.Fatalf("stream err = %v", err)
	}
	if err := e.VerifyKey(context.Background()); err == nil || !strings.Contains(err.Error(), "rejected the API key") {
		t.Fatalf("verify = %v", err)
	}
}

func TestGladiaTranscribeFile(t *testing.T) {
	var mu sync.Mutex
	var job map[string]any
	polls, deleted := 0, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/upload":
			_ = r.ParseMultipartForm(1 << 20)
			if _, ok := r.MultipartForm.File["audio"]; !ok {
				t.Error("no audio part")
			}
			_, _ = io.WriteString(w, `{"audio_url":"https://api.gladia.io/file/abc"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/pre-recorded":
			_ = json.NewDecoder(r.Body).Decode(&job)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"j1","result_url":"x"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/pre-recorded/j1":
			polls++
			if polls == 1 {
				_, _ = io.WriteString(w, `{"status":"processing"}`)
				return
			}
			_, _ = io.WriteString(w, `{"status":"done","result":{"transcription":{"utterances":[
			  {"text":"Xin chào.","speaker":0,"start":0.5,"end":1.0,"language":"vi"},
			  {"text":"Chào anh.","speaker":1,"start":1.2,"end":1.6,"language":"vi"}]}}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/pre-recorded/j1":
			deleted = true
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "a.mp3")
	_ = os.WriteFile(path, []byte("ID3"), 0o600)
	e := newGladiaEngine("gl-key", srv.URL)
	e.poll = 10 * time.Millisecond
	us, err := e.TranscribeFile(context.Background(), path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if job["audio_url"] != "https://api.gladia.io/file/abc" || job["diarization"] != true || !deleted {
		t.Fatalf("job %v deleted %v", job, deleted)
	}
	if len(us) != 2 || us[0].Speaker != "Speaker 1" || us[1].Speaker != "Speaker 2" || us[1].At.Sub(time.Time{}) != 1200*time.Millisecond {
		t.Fatalf("utterances = %+v", us)
	}
}
