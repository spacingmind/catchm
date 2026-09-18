package translate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListModelsReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{"name": "translategemma:4b"},
				{"name": "translategemma:12b"},
			},
		})
	}))
	defer srv.Close()

	got, err := ListModels(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"translategemma:4b", "translategemma:12b"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got = %v, want %v", got, want)
	}
}

func TestListModelsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	host := srv.URL
	srv.Close()

	_, err := ListModels(context.Background(), host)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrOllamaUnreachable) {
		t.Fatalf("err = %v, want it to wrap ErrOllamaUnreachable", err)
	}
}

func TestPullStreamsProgress(t *testing.T) {
	lines := []string{
		`{"status":"pulling manifest"}`,
		`{"status":"downloading 42%"}`,
		`{"status":"success"}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pull" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["model"] != "translategemma:4b" {
			t.Fatalf("model = %v", req["model"])
		}
		if req["stream"] != true {
			t.Fatalf("stream = %v, want true", req["stream"])
		}
		for _, line := range lines {
			fmt.Fprintln(w, line)
		}
	}))
	defer srv.Close()

	var got []string
	err := Pull(context.Background(), srv.URL, "translategemma:4b", func(status string) {
		got = append(got, status)
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pulling manifest", "downloading 42%", "success"}
	if len(got) != len(want) {
		t.Fatalf("got = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPullPropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{"status":"pulling manifest"}`)
		fmt.Fprintln(w, `{"error":"model not found"}`)
	}))
	defer srv.Close()

	err := Pull(context.Background(), srv.URL, "nonexistent:1b", func(string) {})
	if err == nil {
		t.Fatal("expected error")
	}
}
