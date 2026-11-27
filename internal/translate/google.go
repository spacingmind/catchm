package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// GoogleEndpoint is the Cloud Translation Basic (v2) REST endpoint.
const GoogleEndpoint = "https://translation.googleapis.com/language/translate/v2"

// GoogleTranslator uses Google Cloud Translation Basic (v2) with an API
// key. First 500,000 characters a month are free (input and output
// metered separately), then $20 per 1M.
type GoogleTranslator struct {
	APIKey   string
	Endpoint string // GoogleEndpoint when empty; tests point it at a fake
	Client   *http.Client
}

type googleRequest struct {
	Q      []string `json:"q"`
	Target string   `json:"target"`
	Format string   `json:"format"`
}

type googleResponse struct {
	Data struct {
		Translations []struct {
			TranslatedText string `json:"translatedText"`
		} `json:"translations"`
	} `json:"data"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Translate sends text as plain text ("format":"text", so no HTML
// entities come back) and lets Google detect the source language.
func (g *GoogleTranslator) Translate(ctx context.Context, text, targetLang string) (string, error) {
	if g.APIKey == "" {
		return "", fmt.Errorf("google translate: no API key set")
	}
	endpoint := g.Endpoint
	if endpoint == "" {
		endpoint = GoogleEndpoint
	}
	body, err := json.Marshal(googleRequest{Q: []string{text}, Target: googleCode(targetLang), Format: "text"})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?key="+url.QueryEscape(g.APIKey), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client(g.Client).Do(req)
	if err != nil {
		return "", fmt.Errorf("google translate: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var gr googleResponse
	_ = json.Unmarshal(raw, &gr)
	if resp.StatusCode != http.StatusOK || gr.Error != nil {
		msg := strings.TrimSpace(string(raw))
		if gr.Error != nil && gr.Error.Message != "" {
			msg = gr.Error.Message
		}
		return "", fmt.Errorf("google translate: status %d: %s", resp.StatusCode, msg)
	}
	if len(gr.Data.Translations) == 0 {
		return "", fmt.Errorf("google translate: empty response")
	}
	return strings.TrimSpace(gr.Data.Translations[0].TranslatedText), nil
}

// googleCode maps CatchM's language codes (web/src/lib/languages.ts) to
// Google's: they match except Chinese, which Google wants as zh-CN.
func googleCode(code string) string {
	if code == "zh" {
		return "zh-CN"
	}
	return code
}

func client(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return http.DefaultClient
}

// VerifyGoogle checks a key with the free languages listing (no
// characters translated, so nothing is billed).
func VerifyGoogle(ctx context.Context, c *http.Client, endpoint, apiKey string) error {
	if endpoint == "" {
		endpoint = GoogleEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/languages?key="+url.QueryEscape(apiKey), nil)
	if err != nil {
		return err
	}
	resp, err := client(c).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var gr googleResponse
	if json.Unmarshal(raw, &gr) == nil && gr.Error != nil && gr.Error.Message != "" {
		return fmt.Errorf("status %d: %s", resp.StatusCode, gr.Error.Message)
	}
	return fmt.Errorf("status %d", resp.StatusCode)
}
