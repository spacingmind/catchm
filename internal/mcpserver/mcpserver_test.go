package mcpserver

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/tools"
)

func fixture(t *testing.T) (*tools.Surface, string) {
	t.Helper()
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	m, err := store.Start("Q4 planning", start, meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(m.ID, meeting.Utterance{At: start.Add(65 * time.Second), Speaker: "1", Text: "Let's ship pricing.", Translated: "Hãy ra mắt bảng giá."}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateDoc(m.ID, func(d *meeting.Meeting) error {
		d.Summary = "We agreed to ship pricing."
		d.Tags = meeting.WithProject([]string{"roadmap"}, "Atlas")
		d.Decisions = []meeting.Decision{{Text: "Ship pricing in Q4"}}
		d.ActionItems = []meeting.ActionItem{{Text: "Draft one-pager", Owner: "An", Due: "2026-12-01"}, {Text: "Old task", Status: "done"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.End(m.ID, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	return tools.New(store), m.ID
}

func connect(t *testing.T, s *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := s.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
	}
	sort.Strings(names)
	return names
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if out != nil && !res.IsError {
		b, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s: decode %s: %v", name, b, err)
		}
	}
	return res
}

func TestReadOnlyByDefault(t *testing.T) {
	surface, _ := fixture(t)
	cs := connect(t, New(surface, Options{}))
	got := strings.Join(toolNames(t, cs), ",")
	if got != "get_meeting,list_action_items,list_meetings,list_projects,read_transcript,search_meetings" {
		t.Fatalf("tools = %s", got)
	}
	cw := connect(t, New(surface, Options{AllowWrites: true}))
	if n := len(toolNames(t, cw)); n != 11 {
		t.Fatalf("with writes: %d tools", n)
	}
}

func TestReadTools(t *testing.T) {
	surface, id := fixture(t)
	cs := connect(t, New(surface, Options{}))

	var list MeetingList
	call(t, cs, "list_meetings", map[string]any{"project": "atlas"}, &list)
	if list.Total != 1 || list.Meetings[0].ID != id || list.Meetings[0].Project != "Atlas" || strings.Join(list.Meetings[0].Tags, ",") != "roadmap" || !list.Meetings[0].HasSum {
		t.Fatalf("list = %+v", list)
	}
	call(t, cs, "search_meetings", map[string]any{"query": "PRICING"}, &list)
	if list.Total != 1 {
		t.Fatalf("search = %+v", list)
	}

	var d MeetingDetail
	call(t, cs, "get_meeting", map[string]any{"id": id}, &d)
	if d.Summary != "We agreed to ship pricing." || len(d.Decisions) != 1 || len(d.ActionItems) != 2 {
		t.Fatalf("detail = %+v", d)
	}
	if res := call(t, cs, "get_meeting", map[string]any{"id": "nope"}, nil); !res.IsError {
		t.Fatal("unknown id did not error")
	}

	var tr Transcript
	call(t, cs, "read_transcript", map[string]any{"id": id}, &tr)
	if tr.Source != "live" || len(tr.Lines) != 1 || tr.Lines[0].At != "0:01:05" || tr.Lines[0].Translated == "" {
		t.Fatalf("transcript = %+v", tr)
	}

	var ai ActionItems
	call(t, cs, "list_action_items", nil, &ai)
	if len(ai.Items) != 1 || ai.Items[0].Owner != "An" || ai.Items[0].Project != "Atlas" {
		t.Fatalf("open items = %+v", ai)
	}
	call(t, cs, "list_action_items", map[string]any{"open_only": false}, &ai)
	if len(ai.Items) != 2 {
		t.Fatalf("all items = %+v", ai)
	}

	var ps Projects
	call(t, cs, "list_projects", nil, &ps)
	if len(ps.Projects) != 1 || ps.Projects[0].Name != "Atlas" {
		t.Fatalf("projects = %+v", ps)
	}
}

func TestWriteTools(t *testing.T) {
	surface, id := fixture(t)
	cs := connect(t, New(surface, Options{AllowWrites: true}))
	call(t, cs, "add_note", map[string]any{"id": id, "note": "Follow up Friday"}, nil)
	call(t, cs, "rename_meeting", map[string]any{"id": id, "title": "Q4 pricing"}, nil)
	call(t, cs, "set_meeting_tags", map[string]any{"id": id, "tags": []string{"pricing"}}, nil)
	m, err := surface.GetMeeting(id)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Q4 pricing" || len(m.Notes) != 1 || meeting.ProjectOf(m.Tags) != "Atlas" || !strings.Contains(strings.Join(m.Tags, ","), "pricing") {
		t.Fatalf("after writes: %+v", m)
	}
}
