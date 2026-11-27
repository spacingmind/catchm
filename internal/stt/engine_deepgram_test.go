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
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// dgResults builds a Deepgram "Results" message (shape per
// developers.deepgram.com streaming reference).
func dgResults(isFinal, speechFinal bool, transcript string, words ...[3]any) []byte {
	ws := []map[string]any{}
	for _, w := range words {
		m := map[string]any{"word": strings.ToLower(w[0].(string)), "punctuated_word": w[0]}
		if w[1] != nil {
			m["speaker"] = w[1]
		}
		ws = append(ws, m)
	}
	b, _ := json.Marshal(map[string]any{
		"type": "Results", "is_final": isFinal, "speech_final": speechFinal,
		"channel": map[string]any{"alternatives": []any{map[string]any{"transcript": transcript, "words": ws}}},
	})
	return b
}

func TestDeepgramStream(t *testing.T) {
	var gotAuth, gotQuery string
	audioFrames := 0
	closeStream := make(chan struct{})
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotQuery = r.Header.Get("Authorization"), r.URL.RawQuery
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		// Read audio until CloseStream, answering as Deepgram does.
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if mt == websocket.BinaryMessage {
				audioFrames++
				if audioFrames == 1 {
					// An interim (ignored), then two finals by two speakers
					// closed by speech_final, then a final closed by UtteranceEnd.
					_ = conn.WriteMessage(websocket.TextMessage, dgResults(false, false, "hel", [3]any{"hel", 0, nil}))
					_ = conn.WriteMessage(websocket.TextMessage, dgResults(true, false, "Hello there.", [3]any{"Hello", 0, nil}, [3]any{"there.", 0, nil}))
					_ = conn.WriteMessage(websocket.TextMessage, dgResults(true, true, "Hi Anna.", [3]any{"Hi", 1, nil}, [3]any{"Anna.", 1, nil}))
					_ = conn.WriteMessage(websocket.TextMessage, dgResults(true, false, "Xin chào.", [3]any{"Xin", 0, nil}, [3]any{"chào.", 0, nil}))
					_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"UtteranceEnd","channel":[0,1],"last_word_end":2.4}`))
				}
				continue
			}
			var m map[string]any
			_ = json.Unmarshal(data, &m)
			if m["type"] == "CloseStream" {
				close(closeStream)
				_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"Metadata","request_id":"r1"}`))
				_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
		}
	}))
	defer srv.Close()

	e := newDeepgramEngine("dg-key", "ws"+strings.TrimPrefix(srv.URL, "http"), srv.URL)
	send, results, err := e.Stream(context.Background(), Options{Lang: "vi", ContextTerms: []string{"CatchM", "Soniox, Inc"}})
	if err != nil {
		t.Fatal(err)
	}
	send <- PCM(make([]byte, 640))
	var got []Result
	done := make(chan struct{})
	go func() {
		for r := range results {
			got = append(got, r)
			if len(got) == 3 {
				close(send)
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("results never closed; got %+v", got)
	}
	select {
	case <-closeStream:
	default:
		t.Error("CloseStream not sent on end of audio")
	}

	if gotAuth != "Token dg-key" {
		t.Errorf("auth = %q", gotAuth)
	}
	for _, want := range []string{"model=nova-3", "language=vi", "encoding=linear16", "sample_rate=16000", "diarize=true", "utterance_end_ms=1000", "mip_opt_out=true", "keyterm=CatchM", "keyterm=Soniox++Inc"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %s", gotQuery, want)
		}
	}
	want := []Result{
		{Text: "Hello there.", Speaker: "1", Final: true},
		{Text: "Hi Anna.", Speaker: "2", Final: true},
		{Text: "Xin chào.", Speaker: "1", Final: true},
	}
	if len(got) != len(want) {
		t.Fatalf("results = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("result %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if st, _ := e.EngineStatus(); st != EngineStreaming {
		t.Errorf("status after a clean end = %q", st)
	}
}

func TestDeepgramAutoLanguageUsesMulti(t *testing.T) {
	if deepgramLanguage(Options{}) != "multi" || deepgramLanguage(Options{Lang: "en", TwoWay: true, LanguageB: "vi"}) != "multi" || deepgramLanguage(Options{Lang: "en"}) != "en" {
		t.Fatal("language mapping")
	}
}

func TestDeepgramBadKeyHandshake(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("dg-error", "Invalid credentials.")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"err_code":"INVALID_AUTH","err_msg":"Invalid credentials.","request_id":"x"}`)
	}))
	defer srv.Close()
	e := newDeepgramEngine("bad", "ws"+strings.TrimPrefix(srv.URL, "http"), srv.URL)
	_, _, err := e.Stream(context.Background(), Options{})
	if err == nil || !strings.Contains(err.Error(), "rejected the API key") || !strings.Contains(err.Error(), "Invalid credentials.") {
		t.Fatalf("err = %v", err)
	}
	if err := e.VerifyKey(context.Background()); err == nil || !strings.Contains(err.Error(), "rejected the API key") {
		t.Fatalf("verify = %v", err)
	}
}

func TestDeepgramTranscribeFile(t *testing.T) {
	var gotQuery, gotCT string
	var gotLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/listen" || r.Header.Get("Authorization") != "Token dg-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		gotQuery, gotCT = r.URL.RawQuery, r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotLen = len(b)
		_, _ = io.WriteString(w, `{"metadata":{"request_id":"r"},"results":{"channels":[{"detected_language":"vi","alternatives":[]}],
		  "utterances":[{"start":0.5,"end":2.25,"transcript":"Xin chào mọi người.","speaker":0},
		                {"start":3,"end":4,"transcript":"  ","speaker":1},
		                {"start":4.5,"end":6,"transcript":"Chào anh.","speaker":1}]}}`)
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "audio.mp3")
	if err := os.WriteFile(path, []byte("ID3fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := newDeepgramEngine("dg-key", "", srv.URL)
	us, err := e.TranscribeFile(context.Background(), path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "detect_language=true") || !strings.Contains(gotQuery, "utterances=true") || !strings.Contains(gotQuery, "mip_opt_out=true") || gotCT != "audio/mpeg" || gotLen != 7 {
		t.Fatalf("request: %q %q %d", gotQuery, gotCT, gotLen)
	}
	if len(us) != 2 || us[0].Text != "Xin chào mọi người." || us[0].Speaker != "Speaker 1" || us[1].Speaker != "Speaker 2" || us[0].Lang != "vi" {
		t.Fatalf("utterances = %+v", us)
	}
	if got := us[0].At.Sub(time.Time{}); got != 500*time.Millisecond || us[0].End.Sub(time.Time{}) != 2250*time.Millisecond {
		t.Fatalf("timing: at %v end %v", got, us[0].End)
	}
}
