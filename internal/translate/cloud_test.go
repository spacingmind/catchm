package translate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGoogleTranslator(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "good" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key."}}`)
			return
		}
		if r.URL.Path == "/languages" {
			_, _ = io.WriteString(w, `{"data":{"languages":[]}}`)
			return
		}
		var req googleRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Format != "text" || req.Target != "zh-CN" || req.Q[0] != "hello" {
			t.Errorf("request = %+v", req)
		}
		_, _ = io.WriteString(w, `{"data":{"translations":[{"translatedText":" 你好 ","detectedSourceLanguage":"en"}]}}`)
	}))
	defer srv.Close()

	g := &GoogleTranslator{APIKey: "good", Endpoint: srv.URL}
	got, err := g.Translate(context.Background(), "hello", "zh")
	if err != nil || got != "你好" {
		t.Fatalf("Translate = %q, %v", got, err)
	}
	bad := &GoogleTranslator{APIKey: "bad", Endpoint: srv.URL}
	if _, err := bad.Translate(context.Background(), "hello", "vi"); err == nil || !strings.Contains(err.Error(), "API key not valid") {
		t.Fatalf("bad key error = %v", err)
	}
	if err := VerifyGoogle(context.Background(), nil, srv.URL, "good"); err != nil {
		t.Fatalf("verify good: %v", err)
	}
	if err := VerifyGoogle(context.Background(), nil, srv.URL, "bad"); err == nil || !strings.Contains(err.Error(), "API key not valid") {
		t.Fatalf("verify bad = %v", err)
	}
}

func TestLLMTranslator(t *testing.T) {
	var gotReq map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-good" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"Incorrect API key provided"}}`)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"<think>hmm</think>\nXin chào mọi người"}}]}`)
	}))
	defer srv.Close()

	tr := &LLMTranslator{BaseURL: srv.URL + "/v1/", APIKey: "sk-good", Model: "fast"}
	got, err := tr.Translate(context.Background(), "Hello everyone", "vi")
	if err != nil || got != "Xin chào mọi người" {
		t.Fatalf("Translate = %q, %v", got, err)
	}
	if gotReq["model"] != "fast" || gotReq["temperature"] != nil || gotReq["response_format"] != nil {
		t.Fatalf("request = %v (no temperature / response_format allowed)", gotReq)
	}
	msgs, _ := gotReq["messages"].([]any)
	if sys, _ := msgs[0].(map[string]any)["content"].(string); !strings.Contains(sys, "Vietnamese") {
		t.Fatalf("system prompt doesn't name the language: %q", sys)
	}

	bad := &LLMTranslator{BaseURL: srv.URL + "/v1", APIKey: "sk-bad", Model: "fast"}
	if _, err := bad.Translate(context.Background(), "hi", "vi"); err == nil || !strings.Contains(err.Error(), "Incorrect API key") {
		t.Fatalf("bad key error = %v", err)
	}
	if err := VerifyChat(context.Background(), nil, srv.URL+"/v1", "sk-bad", "fast"); err == nil {
		t.Fatal("verify accepted a bad key")
	}
}
