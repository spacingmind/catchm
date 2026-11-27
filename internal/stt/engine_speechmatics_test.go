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

func smWord(content, speaker string, start, end float64) map[string]any {
	return map[string]any{"type": "word", "start_time": start, "end_time": end,
		"alternatives": []any{map[string]any{"content": content, "speaker": speaker, "language": "en"}}}
}

func smPunct(content, speaker string, at float64, eos bool) map[string]any {
	return map[string]any{"type": "punctuation", "start_time": at, "end_time": at, "attaches_to": "previous", "is_eos": eos,
		"alternatives": []any{map[string]any{"content": content, "speaker": speaker}}}
}

func TestSpeechmaticsStream(t *testing.T) {
	var gotAuth string
	var start map[string]any
	var lastSeq float64
	frames := 0
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		send := func(v any) { b, _ := json.Marshal(v); _ = conn.WriteMessage(websocket.TextMessage, b) }
		_, data, _ := conn.ReadMessage()
		_ = json.Unmarshal(data, &start)
		send(map[string]any{"message": "RecognitionStarted", "id": "s1"})
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if mt == websocket.BinaryMessage {
				frames++
				if frames == 1 {
					send(map[string]any{"message": "AddPartialTranscript", "results": []any{smWord("hel", "S1", 0, 0.2)}})
					// A sentence split over two finals, then another speaker.
					send(map[string]any{"message": "AddTranscript", "results": []any{smWord("Hello", "S1", 0, 0.3)}})
					send(map[string]any{"message": "AddTranscript", "results": []any{smWord("there", "S1", 0.3, 0.6), smPunct(".", "S1", 0.6, true)}})
					send(map[string]any{"message": "AddTranscript", "results": []any{smWord("Hi", "S2", 1, 1.2), smWord("Anna", "S2", 1.2, 1.5), smPunct("!", "S2", 1.5, false)}})
				}
				continue
			}
			var m map[string]any
			_ = json.Unmarshal(data, &m)
			if m["message"] == "EndOfStream" {
				lastSeq, _ = m["last_seq_no"].(float64)
				send(map[string]any{"message": "EndOfTranscript"})
				return
			}
		}
	}))
	defer srv.Close()

	e := newSpeechmaticsEngine("sm-key", "ws"+strings.TrimPrefix(srv.URL, "http"), srv.URL)
	send, results, err := e.Stream(context.Background(), Options{Lang: "en", ContextTerms: []string{"CatchM"}})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		send <- PCM(make([]byte, 640))
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
	if gotAuth != "Bearer sm-key" || lastSeq != 2 {
		t.Errorf("auth %q, last_seq_no %v", gotAuth, lastSeq)
	}
	tc, _ := start["transcription_config"].(map[string]any)
	if start["message"] != "StartRecognition" || tc["language"] != "en" || tc["operating_point"] != "enhanced" || tc["diarization"] != "speaker" || tc["additional_vocab"] == nil {
		t.Errorf("StartRecognition = %v", start)
	}
	want := []Result{
		{Text: "Hello there.", Speaker: "1", Final: true, End: 600 * time.Millisecond},
		{Text: "Hi Anna!", Speaker: "2", Final: true, Start: time.Second, End: 1500 * time.Millisecond},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("results = %+v", got)
	}
}

func TestSpeechmaticsAutoUsesMelia(t *testing.T) {
	c := smTranscription(Options{ContextTerms: []string{"x"}})
	if c.Language != "multi" || c.Model != "melia-1" || c.OperatingPoint != "" || c.AdditionalVoc != nil {
		t.Fatalf("auto config = %+v", c)
	}
}

func TestSpeechmaticsBadKey(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/jobs") {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"code":401,"error":"Permission Denied"}`)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"message":"Error","type":"not_authorised","reason":"Invalid API key"}`))
	}))
	defer srv.Close()
	e := newSpeechmaticsEngine("bad", "ws"+strings.TrimPrefix(srv.URL, "http"), srv.URL)
	if _, _, err := e.Stream(context.Background(), Options{Lang: "en"}); err == nil || !strings.Contains(err.Error(), "rejected the API key") {
		t.Fatalf("stream err = %v", err)
	}
	if err := e.VerifyKey(context.Background()); err == nil || !strings.Contains(err.Error(), "rejected the API key") {
		t.Fatalf("verify = %v", err)
	}
}

func TestSpeechmaticsTranscribeFile(t *testing.T) {
	var mu sync.Mutex
	var cfg map[string]any
	polls, deleted := 0, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/jobs":
			_ = r.ParseMultipartForm(1 << 20)
			_ = json.Unmarshal([]byte(r.FormValue("config")), &cfg)
			if _, ok := r.MultipartForm.File["data_file"]; !ok {
				t.Error("no data_file")
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"job1"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/jobs/job1":
			polls++
			status := "running"
			if polls > 1 {
				status = "done"
			}
			_, _ = io.WriteString(w, `{"job":{"status":"`+status+`"}}`)
		case r.URL.Path == "/jobs/job1/transcript":
			b, _ := json.Marshal(map[string]any{"results": []any{
				smWord("Xin", "S1", 0.5, 0.7), smWord("chào", "S1", 0.7, 1), smPunct(".", "S1", 1, true),
				smWord("Chào", "S2", 1.2, 1.4), smWord("anh", "S2", 1.4, 1.6), smPunct(".", "S2", 1.6, true),
				smWord("Khỏe", "S2", 3, 3.2), smWord("không", "S2", 3.2, 3.5), smPunct("?", "S2", 3.5, true),
			}})
			_, _ = w.Write(b)
		case r.Method == http.MethodDelete && r.URL.Path == "/jobs/job1":
			deleted = true
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "audio.wav")
	_ = os.WriteFile(path, []byte("RIFFfake"), 0o600)
	e := newSpeechmaticsEngine("sm-key", "", srv.URL)
	e.poll = 10 * time.Millisecond
	us, err := e.TranscribeFile(context.Background(), path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	tc, _ := cfg["transcription_config"].(map[string]any)
	if tc["language"] != "auto" || tc["diarization"] != "speaker" || !deleted {
		t.Fatalf("config %v, deleted %v", cfg, deleted)
	}
	var texts []string
	for _, u := range us {
		texts = append(texts, u.Speaker+"|"+u.Text)
	}
	if strings.Join(texts, ",") != "Speaker 1|Xin chào.,Speaker 2|Chào anh.,Speaker 2|Khỏe không?" {
		t.Fatalf("utterances = %v", texts)
	}
}
