package summarize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// openaiBudgetTokens caps each OpenAI-compatible call's estimated prompt
// size; it fits models with a 32k context and leaves reply headroom.
const openaiBudgetTokens = 24000

// Response format levels, tried in order until the server accepts one.
const (
	formatJSONSchema = iota
	formatJSONObject
	formatNone
)

// OpenAI summarizes through any OpenAI-compatible chat completions
// server (OpenRouter, a Workers AI key, ...).
type OpenAI struct {
	// BaseURL includes the API version path, e.g.
	// https://openrouter.ai/api/v1.
	BaseURL string
	Model   string
	APIKey  string
	// HTTP defaults to http.DefaultClient.
	HTTP *http.Client

	// format is the first response_format level still worth trying; it
	// only moves forward, so a chunked run doesn't re-probe per call.
	format int
}

func (o *OpenAI) Summarize(ctx context.Context, in Input) (Extraction, error) {
	if o.BaseURL == "" || o.Model == "" {
		return Extraction{}, notReady("Add the server address and model name in Settings → Summaries.", nil)
	}
	return runLLM(ctx, o, openaiBudgetTokens, in)
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiRequest struct {
	Model          string          `json:"model"`
	Messages       []openaiMessage `json:"messages"`
	Temperature    float64         `json:"temperature"`
	ResponseFormat any             `json:"response_format,omitempty"`
}

func responseFormat(level int) any {
	switch level {
	case formatJSONSchema:
		return map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "meeting_extraction",
				"strict": true,
				"schema": schema,
			},
		}
	case formatJSONObject:
		return map[string]any{"type": "json_object"}
	default:
		return nil
	}
}

func (o *OpenAI) complete(ctx context.Context, system, user string) (string, error) {
	for {
		status, raw, err := o.post(ctx, system, user, responseFormat(o.format))
		if err != nil {
			return "", err
		}
		if status == http.StatusOK {
			return openaiContent(raw)
		}
		if o.format < formatNone && rejectsResponseFormat(status, raw) {
			o.format++
			continue
		}
		return "", openaiStatusError(o.BaseURL, status, raw)
	}
}

func (o *OpenAI) post(ctx context.Context, system, user string, format any) (int, []byte, error) {
	body, err := json.Marshal(openaiRequest{
		Model: o.Model,
		Messages: []openaiMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature:    0,
		ResponseFormat: format,
	})
	if err != nil {
		return 0, nil, failed("Couldn't build the summary request.", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(o.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return 0, nil, notReady(fmt.Sprintf("The server address %q isn't valid. Fix it in Settings → Summaries.", o.BaseURL), err)
	}
	req.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}
	resp, err := httpClient(o.HTTP).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, failed("The summary was cancelled.", err)
		}
		return 0, nil, notReady(fmt.Sprintf("Can't reach the summary server at %s. Check that it's running.", o.BaseURL), err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, failed("Lost the connection to the summary server.", err)
	}
	return resp.StatusCode, raw, nil
}

// rejectsResponseFormat reports whether a 4xx reply looks like the
// server refusing our response_format rather than the request itself.
func rejectsResponseFormat(status int, raw []byte) bool {
	if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
		return false
	}
	body := strings.ToLower(string(raw))
	for _, hint := range []string{"response_format", "json_schema", "json_object", "structured output"} {
		if strings.Contains(body, hint) {
			return true
		}
	}
	return false
}

func openaiContent(raw []byte) (string, error) {
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", failed("The summary server sent a reply CatchM couldn't read.", err)
	}
	if len(out.Choices) == 0 {
		// An empty reply is a bad reply, not a transport failure: let
		// the JSON retry have a go.
		return "", nil
	}
	return out.Choices[0].Message.Content, nil
}

func openaiStatusError(baseURL string, status int, raw []byte) error {
	cause := fmt.Errorf("openai-compatible: status %d: %s", status, truncate(strings.TrimSpace(string(raw)), 500))
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return notReady("The summary server rejected the API key. Check it in Settings → Summaries.", cause)
	case http.StatusNotFound:
		return notReady(fmt.Sprintf("The summary server at %s doesn't know this address or model. Check both in Settings → Summaries.", baseURL), cause)
	case http.StatusTooManyRequests:
		return failed("The summary server is rate limiting requests. Try again in a minute.", cause)
	default:
		return failed("The summary server couldn't write the summary.", cause)
	}
}
