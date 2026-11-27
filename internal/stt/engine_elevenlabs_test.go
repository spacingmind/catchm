package stt

import (
	"context"
	"encoding/base64"
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

func TestElevenLabsStream(t *testing.T) {
	var gotKey, gotQuery string
	var audioBytes int
	var commits int
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotQuery = r.Header.Get("xi-api-key"), r.URL.RawQuery
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		send := func(v any) { b, _ := json.Marshal(v); _ = conn.WriteMessage(websocket.TextMessage, b) }
		send(map[string]any{"message_type": "session_started", "session_id": "s1"})
		first := true
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m elevenLabsChunk
			_ = json.Unmarshal(data, &m)
			b, _ := base64.StdEncoding.DecodeString(m.Audio)
			audioBytes += len(b)
			if m.SampleRate != 16000 || m.MessageType != "input_audio_chunk" {
				t.Errorf("chunk = %+v", m)
			}
			if first {
				first = false
				send(map[string]any{"message_type": "partial_transcript", "text": "hel"})
				send(map[string]any{"message_type": "committed_transcript", "text": "Hello there. Hi Anna.", "segment_index": 0})
				send(map[string]any{"message_type": "speakers_merged", "from_speaker_id": "speaker_2", "to_speaker_id": "speaker_1"})
				send(map[string]any{"message_type": "diarized_transcript", "segment_index": 0, "words": []map[string]any{
					{"text": "Hello", "type": "word", "speaker_id": "speaker_0"}, {"text": " ", "type": "spacing"},
					{"text": "there.", "type": "word", "speaker_id": "speaker_0"}, {"text": " ", "type": "spacing"},
					{"text": "Hi", "type": "word", "speaker_id": "speaker_2"}, {"text": " ", "type": "spacing"},
					{"text": "Anna.", "type": "word", "speaker_id": "speaker_2"},
				}})
			}
			if m.Commit {
				commits++
				// No diarization for this one: it must still come out.
				send(map[string]any{"message_type": "committed_transcript", "text": "Bye.", "segment_index": 1})
			}
		}
	}))
	defer srv.Close()

	e := newElevenLabsEngine("xi-key", "ws"+strings.TrimPrefix(srv.URL, "http"), srv.URL)
	send, results, err := e.Stream(context.Background(), Options{Lang: "vi", ContextTerms: []string{"CatchM"}})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for i := 0; i < 12; i++ { // one full 10-frame chunk + 2 left for the final commit
			send <- PCM(make([]byte, 640))
		}
		time.Sleep(200 * time.Millisecond)
		close(send)
	}()
	var got []Result
	timeout := time.After(10 * time.Second)
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
	if gotKey != "xi-key" {
		t.Errorf("key header = %q", gotKey)
	}
	for _, want := range []string{"model_id=scribe_v3_live", "audio_format=pcm_16000", "commit_strategy=vad", "diarize=true", "language_code=vie", "keyterms=CatchM"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %s", gotQuery, want)
		}
	}
	if audioBytes != 12*640 || commits != 1 {
		t.Errorf("audio %d bytes, %d commits", audioBytes, commits)
	}
	want := []Result{
		{Text: "Hello there.", Speaker: "1", Final: true},
		{Text: "Hi Anna.", Speaker: "2", Final: true}, // speaker_2 merged into speaker_1 → "2"
		{Text: "Bye.", Final: true},
	}
	if len(got) != len(want) {
		t.Fatalf("results = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("result %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestElevenLabsAuthError(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"detail":{"status":"invalid_api_key","message":"Invalid API key"}}`)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"message_type":"auth_error","error":"Invalid API key"}`))
	}))
	defer srv.Close()
	e := newElevenLabsEngine("bad", "ws"+strings.TrimPrefix(srv.URL, "http"), srv.URL)
	if _, _, err := e.Stream(context.Background(), Options{}); err == nil || !strings.Contains(err.Error(), "rejected the API key") {
		t.Fatalf("stream err = %v", err)
	}
	if err := e.VerifyKey(context.Background()); err == nil || !strings.Contains(err.Error(), "rejected the API key") {
		t.Fatalf("verify = %v", err)
	}
}

func TestElevenLabsTranscribeFile(t *testing.T) {
	var fields map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/speech-to-text" || r.Header.Get("xi-api-key") != "xi-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("multipart: %v", err)
		}
		fields = r.MultipartForm.Value
		if _, ok := r.MultipartForm.File["file"]; !ok {
			t.Error("no file part")
		}
		_, _ = io.WriteString(w, `{"language_code":"vie","text":"...","words":[
		 {"text":"Xin","type":"word","start":0.1,"end":0.3,"speaker_id":"speaker_0"},{"text":" ","type":"spacing"},
		 {"text":"chào.","type":"word","start":0.3,"end":0.6,"speaker_id":"speaker_0"},{"text":" ","type":"spacing"},
		 {"text":"(laughs)","type":"audio_event","start":0.6,"end":0.9},
		 {"text":"Chào","type":"word","start":1.0,"end":1.2,"speaker_id":"speaker_1"},{"text":" ","type":"spacing"},
		 {"text":"anh.","type":"word","start":1.2,"end":1.5,"speaker_id":"speaker_1"},{"text":" ","type":"spacing"},
		 {"text":"Khỏe","type":"word","start":3.0,"end":3.3,"speaker_id":"speaker_1"},{"text":" ","type":"spacing"},
		 {"text":"không?","type":"word","start":3.3,"end":3.7,"speaker_id":"speaker_1"}]}`)
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "audio.wav")
	_ = os.WriteFile(path, []byte("RIFFfake"), 0o600)
	us, err := newElevenLabsEngine("xi-key", "", srv.URL).TranscribeFile(context.Background(), path, Options{Lang: "vi"})
	if err != nil {
		t.Fatal(err)
	}
	if fields["model_id"][0] != "scribe_v3" || fields["diarize"][0] != "true" || fields["language_code"][0] != "vie" {
		t.Fatalf("fields = %v", fields)
	}
	texts := []string{}
	for _, u := range us {
		texts = append(texts, u.Speaker+"|"+u.Text)
	}
	want := "Speaker 1|Xin chào.,Speaker 2|Chào anh.,Speaker 2|Khỏe không?"
	if strings.Join(texts, ",") != want {
		t.Fatalf("utterances = %v", texts)
	}
	if us[1].At.Sub(time.Time{}) != time.Second || us[0].Lang != "vi" {
		t.Fatalf("timing/lang: %v %q", us[1].At.Sub(time.Time{}), us[0].Lang)
	}
}
