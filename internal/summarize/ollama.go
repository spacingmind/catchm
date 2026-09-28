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

const (
	// ollamaBudgetTokens caps each Ollama call's estimated prompt size;
	// longer transcripts are chunked. It keeps num_ctx (and so the
	// model's memory use) modest on a laptop.
	ollamaBudgetTokens = 12000
	// ollamaReplyTokens is the context headroom left for the reply.
	ollamaReplyTokens = 6144
	// ollamaMinCtx is the smallest num_ctx requested, so short meetings
	// don't thrash the model's context size between calls.
	ollamaMinCtx = 8192
)

// Ollama summarizes with a local model through Ollama's native
// /api/chat, constraining the reply with the analysis JSON schema.
type Ollama struct {
	Host  string
	Model string
	// HTTP defaults to http.DefaultClient.
	HTTP *http.Client
}

func (o *Ollama) Summarize(ctx context.Context, in Input) (Analysis, error) {
	if o.Host == "" || o.Model == "" {
		return Analysis{}, notReady("Pick an Ollama model in Settings → Summaries.", nil)
	}
	return runLLM(ctx, o, ollamaBudgetTokens, in)
}

func (o *Ollama) describe() (string, string) { return ProviderOllama, o.Model }

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Format   any             `json:"format"`
	Stream   bool            `json:"stream"`
	// Think false keeps reasoning models (qwen3.x) from spending the
	// reply on a hidden chain of thought; older Ollama ignores it.
	Think   bool          `json:"think"`
	Options ollamaOptions `json:"options"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature"`
	NumCtx      int     `json:"num_ctx"`
}

// ollamaNumCtx sizes the context window for a prompt: its estimated
// tokens plus reply headroom, rounded up to a multiple of 1024.
func ollamaNumCtx(system, user string) int {
	n := estimateTokens(system) + estimateTokens(user) + ollamaReplyTokens
	n = (n + 1023) / 1024 * 1024
	return max(n, ollamaMinCtx)
}

func (o *Ollama) complete(ctx context.Context, f replyFormat, system, user string) (string, error) {
	body, err := json.Marshal(ollamaChatRequest{
		Model: o.Model,
		Messages: []ollamaMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Format:  f.schema,
		Stream:  false,
		Options: ollamaOptions{Temperature: 0, NumCtx: ollamaNumCtx(system, user)},
	})
	if err != nil {
		return "", failed("Couldn't build the summary request.", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(o.Host, "/")+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", notReady(fmt.Sprintf("The Ollama address %q isn't valid. Fix it in Settings → Translation.", o.Host), err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient(o.HTTP).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", failed("The summary was cancelled.", err)
		}
		return "", notReady("Ollama isn't running. Start Ollama, then try again.", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", failed("Lost the connection to Ollama while summarizing.", err)
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		cause := fmt.Errorf("ollama: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		if resp.StatusCode == http.StatusNotFound || strings.Contains(e.Error, "not found") {
			return "", notReady(fmt.Sprintf("The summary model %s isn't downloaded yet. Download it in Settings → Summaries.", o.Model), cause)
		}
		return "", failed("Ollama couldn't write the summary.", cause)
	}
	var out struct {
		Message ollamaMessage `json:"message"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", failed("Ollama sent a reply CatchM couldn't read.", err)
	}
	return out.Message.Content, nil
}

func httpClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return http.DefaultClient
}
