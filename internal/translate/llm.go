package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// LLMTranslator translates through any OpenAI-compatible chat
// completions server (config.LLMConfig: OpenAI, Gemini, OpenRouter,
// Anthropic, Groq, DeepSeek, Cloudflare, custom). Plain text in and out:
// no response_format, so every preset accepts the request.
type LLMTranslator struct {
	BaseURL string // includes the version path, e.g. https://api.openai.com/v1
	APIKey  string
	Model   string
	Client  *http.Client
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// No temperature or top_p: Anthropic's compatibility layer rejects
// non-default values on current models, and the defaults are fine here.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// thinkBlock strips reasoning that some models (Qwen3 on Cloudflare,
// DeepSeek) inline before the answer.
var thinkBlock = regexp.MustCompile(`(?s)<think>.*?</think>`)

func (t *LLMTranslator) Translate(ctx context.Context, text, targetLang string) (string, error) {
	if t.BaseURL == "" || t.APIKey == "" || t.Model == "" {
		return "", fmt.Errorf("llm translate: server, model or API key not set")
	}
	system := fmt.Sprintf("You translate meeting transcript lines into %s. "+
		"Reply with only the translation of the user's text: no quotes, notes or explanations. "+
		"Keep names, numbers and technical terms as they are. If the text is already in %s, repeat it unchanged.",
		LanguageName(targetLang), LanguageName(targetLang))
	content, err := ChatComplete(ctx, client(t.Client), t.BaseURL, t.APIKey, chatRequest{
		Model:    t.Model,
		Messages: []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: text}},
	})
	if err != nil {
		return "", fmt.Errorf("llm translate: %w", err)
	}
	return content, nil
}

// ChatComplete posts one non-streaming chat completion and returns the
// first choice's text with any <think> block removed. Errors carry the
// server's own message (bad key, unknown model…).
func ChatComplete(ctx context.Context, c *http.Client, baseURL, apiKey string, body chatRequest) (string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respRaw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var cr chatResponse
	_ = json.Unmarshal(respRaw, &cr)
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(respRaw))
		if cr.Error != nil && cr.Error.Message != "" {
			msg = cr.Error.Message
		}
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, msg)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return strings.TrimSpace(thinkBlock.ReplaceAllString(cr.Choices[0].Message.Content, "")), nil
}

// VerifyChat makes the cheapest possible authenticated call: a one-word
// completion. Used by the settings "Check key" button.
func VerifyChat(ctx context.Context, c *http.Client, baseURL, apiKey, model string) error {
	_, err := ChatComplete(ctx, client(c), baseURL, apiKey, chatRequest{
		Model:    model,
		Messages: []chatMessage{{Role: "user", Content: "Reply with OK."}},
	})
	return err
}

// LanguageName turns CatchM's language codes into English names for
// prompts; unknown codes pass through.
func LanguageName(code string) string {
	if n, ok := languageNames[code]; ok {
		return n
	}
	return code
}

var languageNames = map[string]string{
	"en": "English", "vi": "Vietnamese", "es": "Spanish", "fr": "French", "de": "German",
	"it": "Italian", "pt": "Portuguese", "nl": "Dutch", "ru": "Russian", "uk": "Ukrainian",
	"pl": "Polish", "tr": "Turkish", "ar": "Arabic", "fa": "Persian", "hi": "Hindi",
	"bn": "Bengali", "th": "Thai", "id": "Indonesian", "ms": "Malay", "zh": "Chinese (Simplified)",
	"ja": "Japanese", "ko": "Korean",
}
