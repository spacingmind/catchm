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
// auth header, reads one binary PCM frame, and answers with a message
// carrying a diarized speaker and a translation; the engine must surface
// both fields on the Result.
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
			Text:        "hello there",
			Speaker:     "spk_1",
			Translation: "xin chao",
			IsFinal:     true,
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
