package translate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOllamaTranslate asserts the request body sent to Ollama's
// /api/generate endpoint carries the target language and source text, and
// that the client returns the canned response trimmed.
func TestOllamaTranslate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["model"] != "translategemma:4b" {
			t.Fatalf("model = %v", req["model"])
		}
		if req["stream"] != false {
			t.Fatalf("stream = %v, want false", req["stream"])
		}
		prompt, _ := req["prompt"].(string)
		if !strings.Contains(prompt, "vi") || !strings.Contains(prompt, "hello there") {
			t.Fatalf("prompt = %q, want it to mention target lang and source text", prompt)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"response": "  xin chao  \n"})
	}))
	defer srv.Close()

	tr := &OllamaTranslator{Host: srv.URL, Model: "translategemma:4b"}
	got, err := tr.Translate(context.Background(), "hello there", "vi")
	if err != nil {
		t.Fatal(err)
	}
	if got != "xin chao" {
		t.Fatalf("got = %q, want %q", got, "xin chao")
	}
}

func TestOllamaTranslateServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	tr := &OllamaTranslator{Host: srv.URL, Model: "translategemma:4b"}
	if _, err := tr.Translate(context.Background(), "hello", "vi"); err == nil {
		t.Fatal("expected error on non-200 status")
	}
}
