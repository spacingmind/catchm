package translate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrOllamaUnreachable wraps errors caused by Ollama not being reachable at
// all (connection refused, DNS failure, etc.), as opposed to a reachable
// Ollama returning an application-level error.
var ErrOllamaUnreachable = errors.New("ollama unreachable")

type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// ListModels returns the names of every model Ollama at host currently has
// pulled, via GET /api/tags.
func ListModels(ctx context.Context, host string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, host+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOllamaUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama: status %d", resp.StatusCode)
	}

	var tr ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("ollama: decode response: %w", err)
	}
	names := make([]string, len(tr.Models))
	for i, m := range tr.Models {
		names[i] = m.Name
	}
	return names, nil
}

type ollamaPullRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

type ollamaPullFrame struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

// Pull POSTs a pull request for model to Ollama at host and streams the
// NDJSON progress response, calling onProgress once per line as it
// arrives. It returns an error if any frame carries a non-empty error
// field, or on a non-2xx response.
func Pull(ctx context.Context, host, model string, onProgress func(status string)) error {
	body, err := json.Marshal(ollamaPullRequest{Model: model, Stream: true})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrOllamaUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ollama: status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var frame ollamaPullFrame
		if err := json.Unmarshal(line, &frame); err != nil {
			return fmt.Errorf("ollama: decode pull frame: %w", err)
		}
		if frame.Error != "" {
			return fmt.Errorf("ollama: pull failed: %s", frame.Error)
		}
		if onProgress != nil {
			onProgress(frame.Status)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("ollama: read pull stream: %w", err)
	}
	return nil
}
