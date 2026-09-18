package stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestSonioxSpeakerAndTranslation runs a mock WS server that checks the
// auth header, reads the initial config frame followed by one binary PCM
// frame, and answers with a message carrying a diarized speaker and a
// translation; the engine must surface both fields on the Result.
func TestSonioxSpeakerAndTranslation(t *testing.T) {
	var gotAuth string
	var gotFrame []byte
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		if mt, data, err := conn.ReadMessage(); err != nil {
			t.Errorf("read config frame: %v", err)
			return
		} else if mt != websocket.TextMessage {
			t.Errorf("config frame type = %d, want text", mt)
		} else if !json.Valid(data) {
			t.Errorf("config frame is not valid json: %q", data)
		}

		mt, data, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read frame: %v", err)
			return
		}
		if mt != websocket.BinaryMessage {
			t.Errorf("message type = %d, want binary", mt)
		}
		gotFrame = data

		out, _ := json.Marshal(sonioxMessage{
			Tokens: []sonioxToken{
				{Text: "hello there", Speaker: "spk_1", IsFinal: true},
				{Text: "xin chao", TranslationStatus: "translation", IsFinal: true},
			},
		})
		if err := conn.WriteMessage(websocket.TextMessage, out); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	engine := NewSonioxEngine(wsURL, "test-key")
	if !engine.SupportsDiarization() || !engine.SupportsTranslation() {
		t.Fatal("soniox engine should support diarization and translation")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pcmCh, resultCh, err := engine.Stream(ctx, Options{TranslateTo: "vi"})
	if err != nil {
		t.Fatal(err)
	}

	chunk := PCM{9, 9, 9, 9}
	select {
	case pcmCh <- chunk:
	case <-time.After(time.Second):
		t.Fatal("timed out sending pcm chunk")
	}

	res := recvResult(t, resultCh)
	if res.Text != "hello there" || res.Speaker != "spk_1" || res.Translated != "xin chao" || !res.Final {
		t.Fatalf("result = %+v", res)
	}

	close(pcmCh)

	if gotAuth != "Bearer test-key" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if string(gotFrame) != string(chunk) {
		t.Fatalf("frame = %v, want %v", gotFrame, chunk)
	}
}

// TestSonioxTwoWayConfig captures the first frame the engine sends and
// asserts it is a JSON text frame with the two_way translation object
// carrying the two configured languages, before any binary frame.
func TestSonioxTwoWayConfig(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		mt, data, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read config frame: %v", err)
			return
		}
		if mt != websocket.TextMessage {
			t.Errorf("first frame type = %d, want text", mt)
		}
		var cfg struct {
			APIKey                       string `json:"api_key"`
			Model                        string `json:"model"`
			AudioFormat                  string `json:"audio_format"`
			SampleRate                   int    `json:"sample_rate"`
			NumChannels                  int    `json:"num_channels"`
			EnableLanguageIdentification bool   `json:"enable_language_identification"`
			Translation                  struct {
				Type      string `json:"type"`
				LanguageA string `json:"language_a"`
				LanguageB string `json:"language_b"`
			} `json:"translation"`
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Errorf("config frame not valid json: %v (%q)", err, data)
			return
		}
		if cfg.APIKey != "test-key" {
			t.Errorf("api_key = %q, want %q", cfg.APIKey, "test-key")
		}
		if cfg.Model != "stt-rt-v4" {
			t.Errorf("model = %q, want stt-rt-v4", cfg.Model)
		}
		if cfg.AudioFormat != "pcm_s16le" {
			t.Errorf("audio_format = %q, want pcm_s16le", cfg.AudioFormat)
		}
		if cfg.SampleRate != 16000 {
			t.Errorf("sample_rate = %d, want 16000", cfg.SampleRate)
		}
		if cfg.NumChannels != 1 {
			t.Errorf("num_channels = %d, want 1", cfg.NumChannels)
		}
		if !cfg.EnableLanguageIdentification {
			t.Error("enable_language_identification = false, want true")
		}
		if cfg.Translation.Type != "two_way" {
			t.Errorf("translation.type = %q, want two_way", cfg.Translation.Type)
		}
		if cfg.Translation.LanguageA != "en" {
			t.Errorf("translation.language_a = %q, want en", cfg.Translation.LanguageA)
		}
		if cfg.Translation.LanguageB != "vi" {
			t.Errorf("translation.language_b = %q, want vi", cfg.Translation.LanguageB)
		}

		// The config must arrive before any binary PCM frame: read one
		// more frame and check it is the binary chunk the test sends.
		mt, data, err = conn.ReadMessage()
		if err != nil {
			t.Errorf("read pcm frame: %v", err)
			return
		}
		if mt != websocket.BinaryMessage {
			t.Errorf("second frame type = %d, want binary", mt)
		}
		if string(data) != string(PCM{7, 7}) {
			t.Errorf("pcm frame = %v, want [7 7]", data)
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	engine := NewSonioxEngine(wsURL, "test-key")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pcmCh, _, err := engine.Stream(ctx, Options{Lang: "en", LanguageB: "vi", TwoWay: true, TranslateTo: "must-not-be-used"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case pcmCh <- PCM{7, 7}:
	case <-time.After(time.Second):
		t.Fatal("timed out sending pcm chunk")
	}
	close(pcmCh)
}

// TestSonioxOneWayConfigStillSent proves the config frame is not a
// two-way-only feature: a plain one-way TranslateTo session must also
// open with a JSON text frame, this time with the one_way translation
// object carrying the target language.
func TestSonioxOneWayConfigStillSent(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		mt, data, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read config frame: %v", err)
			return
		}
		if mt != websocket.TextMessage {
			t.Errorf("first frame type = %d, want text", mt)
		}
		var cfg struct {
			Translation struct {
				Type           string `json:"type"`
				TargetLanguage string `json:"target_language"`
				LanguageA      string `json:"language_a"`
				LanguageB      string `json:"language_b"`
			} `json:"translation"`
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Errorf("config frame not valid json: %v (%q)", err, data)
			return
		}
		if cfg.Translation.Type != "one_way" {
			t.Errorf("translation.type = %q, want one_way", cfg.Translation.Type)
		}
		if cfg.Translation.TargetLanguage != "es" {
			t.Errorf("translation.target_language = %q, want es", cfg.Translation.TargetLanguage)
		}
		if cfg.Translation.LanguageA != "" || cfg.Translation.LanguageB != "" {
			t.Errorf("one_way config should not carry language_a/b, got %q/%q", cfg.Translation.LanguageA, cfg.Translation.LanguageB)
		}

		mt, _, err = conn.ReadMessage()
		if err != nil {
			t.Errorf("read pcm frame: %v", err)
			return
		}
		if mt != websocket.BinaryMessage {
			t.Errorf("second frame type = %d, want binary", mt)
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	engine := NewSonioxEngine(wsURL, "test-key")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pcmCh, _, err := engine.Stream(ctx, Options{TranslateTo: "es"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case pcmCh <- PCM{1, 1}:
	case <-time.After(time.Second):
		t.Fatal("timed out sending pcm chunk")
	}
	close(pcmCh)
}

// TestBuildResultGroupsTokensByTranslationStatus proves buildResult
// concatenates original and translated tokens into their own fields
// regardless of interleaving order, keeps the last non-empty speaker,
// and only reports Final once every token in the batch is finalized.
func TestBuildResultGroupsTokensByTranslationStatus(t *testing.T) {
	res := buildResult([]sonioxToken{
		{Text: "hello ", Speaker: "spk_1", IsFinal: true},
		{Text: "xin ", TranslationStatus: "translation", IsFinal: true},
		{Text: "world", IsFinal: true},
		{Text: "chao", TranslationStatus: "translation", IsFinal: true},
	})
	if res.Text != "hello world" {
		t.Errorf("text = %q, want %q", res.Text, "hello world")
	}
	if res.Translated != "xin chao" {
		t.Errorf("translated = %q, want %q", res.Translated, "xin chao")
	}
	if res.Speaker != "spk_1" {
		t.Errorf("speaker = %q, want spk_1", res.Speaker)
	}
	if !res.Final {
		t.Error("final = false, want true (all tokens finalized)")
	}

	partial := buildResult([]sonioxToken{
		{Text: "hel", IsFinal: false},
	})
	if partial.Final {
		t.Error("final = true, want false (non-final token present)")
	}
}
