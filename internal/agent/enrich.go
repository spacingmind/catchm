// Package agent runs enrichment jobs over stored meetings: a Claude Code
// turn with in-process MCP tools that reads the transcript and extracts
// structure into the meeting document's frontmatter.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"

	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/tools"
)

const (
	maxBudgetUSD = 1.0
	maxTurns     = 10
	mcpName      = "catchm"
)

// EnrichMeeting runs one budget-capped Claude Code turn over the meeting:
// the agent reads the meeting and its transcript through the in-process
// MCP tools, returns a JSON object, and Go (not the agent) applies it to
// the frontmatter via UpdateDoc — the agent has no write path.
func EnrichMeeting(ctx context.Context, store *meeting.Store, id string) error {
	if _, err := store.Get(id); err != nil {
		return err
	}

	client, err := claudecode.New(store.Root(),
		// acceptEdits + auto-approve: the run is headless, and the only
		// tools it needs are the two read-only ones below — the SDK's
		// AutoDenyPolicy default would deny those MCP calls themselves.
		claudecode.WithPermissionMode("acceptEdits"),
		// The CLI's session gate blocks MCP tools before the permission
		// policy is consulted (same failure mode as smind's SafeBashRules);
		// pre-approving mcp__catchm__* at spawn time is required for a
		// headless run to ever read the meeting.
		claudecode.WithAllowedTools("mcp__"+mcpName+"__*"),
		claudecode.WithPermissionPolicy(claudecode.AutoApprovePolicy{}),
		claudecode.WithMaxBudgetUSD(maxBudgetUSD),
		claudecode.WithMaxTurns(maxTurns),
		claudecode.WithSDKMcpServer(NewMcpServer(tools.New(store))),
	)
	if err != nil {
		return fmt.Errorf("enrich %s: spawn agent: %w", id, err)
	}
	defer client.Close()

	updates := make(chan claudecode.Message)
	go func() {
		for range updates {
		}
	}()

	result, err := client.Prompt(ctx, enrichPrompt(id), updates)
	if err != nil {
		return fmt.Errorf("enrich %s: agent turn: %w", id, err)
	}
	if result.IsError {
		return fmt.Errorf("enrich %s: agent run failed: %s", id, result.Result)
	}

	ex, err := parseExtraction(result.Result)
	if err != nil {
		return fmt.Errorf("enrich %s: %w", id, err)
	}
	return store.UpdateDoc(id, func(m *meeting.Meeting) error {
		applyExtraction(m, ex)
		return nil
	})
}

// EnrichMeetingFinal runs one budget-capped Claude Code turn over a
// meeting's batch (v2) transcript instead of the live one, marking the
// resulting frontmatter Refined so callers can tell a final enrich pass
// apart from a coarse live one. Otherwise identical to EnrichMeeting.
func EnrichMeetingFinal(ctx context.Context, store *meeting.Store, id string) error {
	if _, err := store.Get(id); err != nil {
		return err
	}

	client, err := claudecode.New(store.Root(),
		claudecode.WithPermissionMode("acceptEdits"),
		claudecode.WithAllowedTools("mcp__"+mcpName+"__*"),
		claudecode.WithPermissionPolicy(claudecode.AutoApprovePolicy{}),
		claudecode.WithMaxBudgetUSD(maxBudgetUSD),
		claudecode.WithMaxTurns(maxTurns),
		claudecode.WithSDKMcpServer(NewMcpServer(tools.New(store))),
	)
	if err != nil {
		return fmt.Errorf("enrich final %s: spawn agent: %w", id, err)
	}
	defer client.Close()

	updates := make(chan claudecode.Message)
	go func() {
		for range updates {
		}
	}()

	result, err := client.Prompt(ctx, enrichFinalPrompt(id), updates)
	if err != nil {
		return fmt.Errorf("enrich final %s: agent turn: %w", id, err)
	}
	if result.IsError {
		return fmt.Errorf("enrich final %s: agent run failed: %s", id, result.Result)
	}

	ex, err := parseExtraction(result.Result)
	if err != nil {
		return fmt.Errorf("enrich final %s: %w", id, err)
	}
	return store.UpdateDoc(id, func(m *meeting.Meeting) error {
		applyExtraction(m, ex)
		m.Refined = true
		return nil
	})
}

// extraction is the JSON shape the agent is instructed to return.
type extraction struct {
	Summary      string               `json:"summary"`
	Participants []string             `json:"participants"`
	Decisions    []meeting.Decision   `json:"decisions"`
	ActionItems  []meeting.ActionItem `json:"action_items"`
}

func enrichPrompt(id string) string {
	return fmt.Sprintf(`Enrich the meeting with id "%s".

Call the get_meeting tool to see its current document, then the
read_live_transcript tool with id "%s" and cursor -1 to read the full
transcript.

Respond with ONLY a JSON object — no markdown fences, no prose outside
the JSON — with exactly these keys:

{
  "summary": "2-3 sentence summary of the meeting",
  "participants": ["speaker names mentioned in the transcript"],
  "decisions": [{"text": "what was decided", "scope": "optional context"}],
  "action_items": [{"text": "task", "owner": "name if stated", "due": "date if stated"}]
}

Use [] for empty arrays. Do not write or edit any files.`, id, id)
}

func enrichFinalPrompt(id string) string {
	return fmt.Sprintf(`Enrich the meeting with id "%s" using its final, high-accuracy transcript.

Call the get_meeting tool to see its current document, then the
read_batch_transcript tool with id "%s" to read the full refined
transcript.

Respond with ONLY a JSON object — no markdown fences, no prose outside
the JSON — with exactly these keys:

{
  "summary": "2-3 sentence summary of the meeting",
  "participants": ["speaker names mentioned in the transcript"],
  "decisions": [{"text": "what was decided", "scope": "optional context"}],
  "action_items": [{"text": "task", "owner": "name if stated", "due": "date if stated"}]
}

Use [] for empty arrays. Do not write or edit any files.`, id, id)
}

// parseExtraction unmarshals the agent's reply, tolerating surrounding
// prose or fences by slicing from the first '{' to the last '}'.
func parseExtraction(raw string) (extraction, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return extraction{}, fmt.Errorf("no JSON object in agent result: %q", raw)
	}
	var ex extraction
	if err := json.Unmarshal([]byte(raw[start:end+1]), &ex); err != nil {
		return extraction{}, fmt.Errorf("parse agent JSON: %w", err)
	}
	return ex, nil
}

// applyExtraction overwrites each field the extraction actually produced;
// absent fields leave the document unchanged.
func applyExtraction(m *meeting.Meeting, ex extraction) {
	if ex.Summary != "" {
		m.Summary = ex.Summary
	}
	if len(ex.Participants) > 0 {
		m.Participants = ex.Participants
	}
	if len(ex.Decisions) > 0 {
		m.Decisions = ex.Decisions
	}
	if len(ex.ActionItems) > 0 {
		m.ActionItems = ex.ActionItems
	}
}

// NewMcpServer builds the in-process MCP server exposing the meeting
// surface to enrichment turns.
func NewMcpServer(s *tools.Surface) *claudecode.SdkMcpServer {
	return claudecode.NewSdkMcpServer(mcpName, "0.1.0",
		claudecode.NewMcpTool(
			"get_meeting",
			"Get a meeting's document: title, participants, decisions, action items, notes.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "meeting id"},
				},
				"required": []string{"id"},
			},
			handleGetMeeting(s),
		),
		claudecode.NewMcpTool(
			"read_live_transcript",
			"Read a meeting's transcript utterances with seq greater than cursor; cursor -1 reads from the beginning.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":     map[string]any{"type": "string", "description": "meeting id"},
					"cursor": map[string]any{"type": "integer", "description": "last seq already read; -1 for all"},
				},
				"required": []string{"id"},
			},
			handleReadLiveTranscript(s),
		),
		claudecode.NewMcpTool(
			"read_batch_transcript",
			"Read a meeting's final, high-accuracy transcript produced by batch refinement over the full recording.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "meeting id"},
				},
				"required": []string{"id"},
			},
			handleReadBatchTranscript(s),
		),
	)
}

func handleGetMeeting(s *tools.Surface) claudecode.McpToolHandler {
	return func(_ context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
		m, err := s.GetMeeting(argString(args, "id"))
		if err != nil {
			return nil, err
		}
		return jsonResult(m)
	}
}

func handleReadLiveTranscript(s *tools.Surface) claudecode.McpToolHandler {
	return func(_ context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
		us, next, err := s.ReadLiveTranscript(argString(args, "id"), argInt64(args, "cursor", -1))
		if err != nil {
			return nil, err
		}
		return jsonResult(map[string]any{"utterances": us, "cursor": next})
	}
}

func handleReadBatchTranscript(s *tools.Surface) claudecode.McpToolHandler {
	return func(_ context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
		us, err := s.ReadBatchTranscript(argString(args, "id"))
		if err != nil {
			return nil, err
		}
		return jsonResult(map[string]any{"utterances": us})
	}
}

func jsonResult(v any) (*claudecode.McpToolResult, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &claudecode.McpToolResult{
		Content: []claudecode.McpContent{{Type: "text", Text: string(raw)}},
	}, nil
}

func argString(args map[string]any, name string) string {
	s, _ := args[name].(string)
	return s
}

// argInt64 reads a numeric tool argument. MCP arguments arrive as
// float64 after JSON decoding, but string-typed numbers are accepted too
// since models occasionally quote them.
func argInt64(args map[string]any, name string, def int64) int64 {
	switch v := args[name].(type) {
	case float64:
		return int64(v)
	case string:
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
