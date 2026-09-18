// Package translate provides text translation for STT engines that have
// no native translation support, via a local TranslateGemma model served
// by Ollama.
package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Translator translates text into targetLang.
type Translator interface {
	Translate(ctx context.Context, text, targetLang string) (string, error)
}

// OllamaTranslator translates text by prompting an Ollama model through
// its /api/generate endpoint.
type OllamaTranslator struct {
	Host  string
	Model string
}

type ollamaGenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
}

// Translate POSTs a translation prompt to Ollama's /api/generate endpoint
// with stream disabled, and returns the model's response trimmed.
func (t *OllamaTranslator) Translate(ctx context.Context, text, targetLang string) (string, error) {
	prompt := fmt.Sprintf("Translate the following text to %s. Respond with only the translation, no commentary or explanation.\n\n%s", targetLang, text)
	body, err := json.Marshal(ollamaGenerateRequest{Model: t.Model, Prompt: prompt, Stream: false})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.Host+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama: status %d", resp.StatusCode)
	}
	var gr ollamaGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return "", fmt.Errorf("ollama: decode response: %w", err)
	}
	return strings.TrimSpace(gr.Response), nil
}
