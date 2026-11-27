// Package mcpserver exposes the meeting tool surface (internal/tools) as
// a Model Context Protocol server, for MCP clients such as Claude
// Desktop, Claude Code and Cursor (docs/plans/completed/local-mcp.md).
// The same server serves stdio (`catchm mcp`) and local Streamable HTTP.
package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/tools"
)

// Options picks what the server offers.
type Options struct {
	// AllowWrites adds add_note, rename_meeting, set_meeting_tags,
	// set_speaker_names and set_meeting_guests.
	AllowWrites bool
	Version     string
}

// New builds an MCP server over surface.
func New(surface *tools.Surface, opts Options) *mcp.Server {
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "catchm", Title: "CatchM meetings", Version: version}, &mcp.ServerOptions{
		Instructions: "CatchM keeps the user's meetings: titles, summaries, decisions, action items, notes and transcripts. " +
			"Use search_meetings or list_meetings to find a meeting, then get_meeting or read_transcript by id.",
	})
	h := &handlers{surface: surface}
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}

	mcp.AddTool(s, &mcp.Tool{Name: "list_meetings", Description: "List meetings, newest first.", Annotations: readOnly}, h.listMeetings)
	mcp.AddTool(s, &mcp.Tool{Name: "search_meetings", Description: "Find meetings whose title, summary, notes, decisions or action items contain the query (case-insensitive).", Annotations: readOnly}, h.searchMeetings)
	mcp.AddTool(s, &mcp.Tool{Name: "get_meeting", Description: "One meeting: times, languages, project, tags, summary, decisions, action items and notes.", Annotations: readOnly}, h.getMeeting)
	mcp.AddTool(s, &mcp.Tool{Name: "read_transcript", Description: "A meeting's transcript, one line per utterance with time, speaker and translation.", Annotations: readOnly}, h.readTranscript)
	mcp.AddTool(s, &mcp.Tool{Name: "list_action_items", Description: "Action items across meetings, with owner, due date and the meeting they came from.", Annotations: readOnly}, h.listActionItems)
	mcp.AddTool(s, &mcp.Tool{Name: "list_projects", Description: "Projects with their meeting counts.", Annotations: readOnly}, h.listProjects)

	if opts.AllowWrites {
		notDestructive := false
		write := &mcp.ToolAnnotations{DestructiveHint: &notDestructive}
		mcp.AddTool(s, &mcp.Tool{Name: "add_note", Description: "Append a note to a meeting.", Annotations: write}, h.addNote)
		mcp.AddTool(s, &mcp.Tool{Name: "rename_meeting", Description: "Change a meeting's title.", Annotations: &mcp.ToolAnnotations{DestructiveHint: &notDestructive, IdempotentHint: true}}, h.renameMeeting)
		mcp.AddTool(s, &mcp.Tool{Name: "set_meeting_tags", Description: "Replace a meeting's tags (keeps its project).", Annotations: &mcp.ToolAnnotations{IdempotentHint: true}}, h.setTags)
		mcp.AddTool(s, &mcp.Tool{Name: "set_speaker_names", Description: "Name a meeting's speakers by key (as get_meeting lists them: \"1\", \"2\", \"me\", \"others\"). An empty name removes one.", Annotations: &mcp.ToolAnnotations{DestructiveHint: &notDestructive, IdempotentHint: true}}, h.setSpeakerNames)
		mcp.AddTool(s, &mcp.Tool{Name: "set_meeting_guests", Description: "Replace a meeting's guest list. Give guests, or text pasted from a calendar invite (\"Name <email>\", one per line or comma-separated).", Annotations: &mcp.ToolAnnotations{IdempotentHint: true}}, h.setGuests)
	}
	return s
}

type handlers struct{ surface *tools.Surface }

// MeetingBrief is one row of a meeting list.
type MeetingBrief struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Project   string     `json:"project,omitempty"`
	Tags      []string   `json:"tags,omitempty"`
	Langs     []string   `json:"langs,omitempty"`
	HasSum    bool       `json:"has_summary"`
}

func brief(m *meeting.Meeting) MeetingBrief {
	return MeetingBrief{
		ID: m.ID, Title: m.Title, StartedAt: m.StartedAt, EndedAt: m.EndedAt,
		Project: meeting.ProjectOf(m.Tags), Tags: plainTags(m.Tags), Langs: m.Langs,
		HasSum: strings.TrimSpace(m.Summary) != "",
	}
}

// plainTags drops the internal "project:" tag (reported as Project).
func plainTags(tags []string) []string {
	var out []string
	for _, t := range tags {
		if !strings.HasPrefix(t, meeting.ProjectTagPrefix) {
			out = append(out, t)
		}
	}
	return out
}

type MeetingList struct {
	Meetings []MeetingBrief `json:"meetings"`
	Total    int            `json:"total"`
}

type ListMeetingsIn struct {
	Limit   int    `json:"limit,omitempty" jsonschema:"how many meetings to return (default 20, max 200)"`
	Project string `json:"project,omitempty" jsonschema:"only meetings in this project (name, case-insensitive)"`
}

func clampLimit(n int) int {
	switch {
	case n <= 0:
		return 20
	case n > 200:
		return 200
	}
	return n
}

func (h *handlers) listMeetings(_ context.Context, _ *mcp.CallToolRequest, in ListMeetingsIn) (*mcp.CallToolResult, MeetingList, error) {
	ms, err := h.surface.ListMeetings()
	if err != nil {
		return nil, MeetingList{}, err
	}
	return nil, toList(ms, in.Project, clampLimit(in.Limit)), nil
}

func toList(ms []*meeting.Meeting, project string, limit int) MeetingList {
	out := MeetingList{Meetings: []MeetingBrief{}}
	for _, m := range ms {
		if project != "" && !strings.EqualFold(meeting.ProjectOf(m.Tags), project) {
			continue
		}
		out.Total++
		if len(out.Meetings) < limit {
			out.Meetings = append(out.Meetings, brief(m))
		}
	}
	return out
}

type SearchIn struct {
	Query string `json:"query" jsonschema:"words to look for"`
	Limit int    `json:"limit,omitempty" jsonschema:"how many meetings to return (default 20, max 200)"`
}

func (h *handlers) searchMeetings(_ context.Context, _ *mcp.CallToolRequest, in SearchIn) (*mcp.CallToolResult, MeetingList, error) {
	if strings.TrimSpace(in.Query) == "" {
		return nil, MeetingList{}, fmt.Errorf("query is empty")
	}
	ms, err := h.surface.SearchMeetings(in.Query)
	if err != nil {
		return nil, MeetingList{}, err
	}
	return nil, toList(ms, "", clampLimit(in.Limit)), nil
}

type IDIn struct {
	ID string `json:"id" jsonschema:"the meeting id (from list_meetings or search_meetings)"`
}

type MeetingDetail struct {
	MeetingBrief
	Participants []string             `json:"participants,omitempty"`
	Guests       []meeting.Guest      `json:"guests,omitempty"`
	Speakers     []SpeakerOut         `json:"speakers,omitempty"`
	Summary      string               `json:"summary,omitempty"`
	Decisions    []meeting.Decision   `json:"decisions,omitempty"`
	ActionItems  []meeting.ActionItem `json:"action_items,omitempty"`
	Notes        []string             `json:"notes,omitempty"`
}

func (h *handlers) getMeeting(_ context.Context, _ *mcp.CallToolRequest, in IDIn) (*mcp.CallToolResult, MeetingDetail, error) {
	m, err := h.surface.GetMeeting(in.ID)
	if err != nil {
		return nil, MeetingDetail{}, notFound(in.ID, err)
	}
	out := MeetingDetail{
		MeetingBrief: brief(m), Participants: m.Participants, Guests: m.Guests, Summary: strings.TrimSpace(m.Summary),
		Decisions: m.Decisions, ActionItems: m.ActionItems, Notes: m.Notes,
	}
	if us, _, err := h.bestTranscript(in.ID); err == nil {
		namer := h.surface.Namer(m)
		for _, st := range meeting.SpeakerStats(us) {
			out.Speakers = append(out.Speakers, SpeakerOut{Key: st.Key, Name: namer.Label(st.Key), Named: m.Speakers[st.Key] != "", Lines: st.Lines})
		}
	}
	return nil, out, nil
}

// SpeakerOut is one speaker of a meeting: its key (for
// set_speaker_names), the name shown, and whether the user confirmed it.
type SpeakerOut struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Named bool   `json:"named"`
	Lines int    `json:"lines"`
}

// bestTranscript is the refined transcript when there is one, else live.
func (h *handlers) bestTranscript(id string) ([]meeting.Utterance, string, error) {
	us, err := h.surface.ReadBatchTranscript(id)
	if err == nil && len(us) > 0 {
		return us, "refined", nil
	}
	us, _, err = h.surface.ReadLiveTranscript(id, -1)
	return us, "live", err
}

func notFound(id string, err error) error {
	if err == meeting.ErrNotFound {
		return fmt.Errorf("no meeting with id %q", id)
	}
	return err
}

type TranscriptIn struct {
	ID     string `json:"id" jsonschema:"the meeting id"`
	Source string `json:"source,omitempty" jsonschema:"best (default: the refined transcript when there is one, else live), live or refined"`
}

type TranscriptLine struct {
	At         string `json:"at"` // offset from the meeting start, h:mm:ss
	Speaker    string `json:"speaker,omitempty"`
	Text       string `json:"text"`
	Translated string `json:"translated,omitempty"`
}

type Transcript struct {
	Source string           `json:"source"`
	Lines  []TranscriptLine `json:"lines"`
}

func (h *handlers) readTranscript(_ context.Context, _ *mcp.CallToolRequest, in TranscriptIn) (*mcp.CallToolResult, Transcript, error) {
	m, err := h.surface.GetMeeting(in.ID)
	if err != nil {
		return nil, Transcript{}, notFound(in.ID, err)
	}
	source := in.Source
	if source == "" {
		source = "best"
	}
	var us []meeting.Utterance
	switch source {
	case "best", "refined":
		us, err = h.surface.ReadBatchTranscript(in.ID)
		if err == nil && len(us) > 0 {
			source = "refined"
			break
		}
		if in.Source == "refined" {
			return nil, Transcript{}, fmt.Errorf("meeting %q has no refined transcript", in.ID)
		}
		fallthrough
	case "live":
		us, _, err = h.surface.ReadLiveTranscript(in.ID, -1)
		source = "live"
	default:
		return nil, Transcript{}, fmt.Errorf(`source must be "best", "live" or "refined"`)
	}
	if err != nil {
		return nil, Transcript{}, err
	}
	out := Transcript{Source: source, Lines: []TranscriptLine{}}
	namer := h.surface.Namer(m)
	for _, u := range us {
		out.Lines = append(out.Lines, TranscriptLine{At: offset(m.StartedAt, u.At), Speaker: namer.Name(u), Text: u.Text, Translated: u.Translated})
	}
	return nil, out, nil
}

// offset renders an utterance's time relative to the meeting start.
// Batch transcripts use offsets from the zero time already.
func offset(start, at time.Time) string {
	d := at.Sub(start)
	if at.Year() < 2 || d < 0 {
		d = at.Sub(time.Time{})
	}
	d = d.Round(time.Second)
	return fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}

type ActionItemsIn struct {
	OpenOnly *bool  `json:"open_only,omitempty" jsonschema:"only items not marked done (default true)"`
	Project  string `json:"project,omitempty" jsonschema:"only items from meetings in this project"`
}

type ActionItemOut struct {
	Text         string `json:"text"`
	Owner        string `json:"owner,omitempty"`
	Due          string `json:"due,omitempty"`
	Status       string `json:"status,omitempty"`
	MeetingID    string `json:"meeting_id"`
	MeetingTitle string `json:"meeting_title"`
	Project      string `json:"project,omitempty"`
}

type ActionItems struct {
	Items []ActionItemOut `json:"items"`
}

func (h *handlers) listActionItems(_ context.Context, _ *mcp.CallToolRequest, in ActionItemsIn) (*mcp.CallToolResult, ActionItems, error) {
	refs, err := h.surface.ListActionItems()
	if err != nil {
		return nil, ActionItems{}, err
	}
	openOnly := in.OpenOnly == nil || *in.OpenOnly
	out := ActionItems{Items: []ActionItemOut{}}
	for _, r := range refs {
		if openOnly && r.Status == "done" {
			continue
		}
		if in.Project != "" && !strings.EqualFold(r.MeetingProject, in.Project) {
			continue
		}
		out.Items = append(out.Items, ActionItemOut{Text: r.Text, Owner: r.Owner, Due: r.Due, Status: r.Status, MeetingID: r.MeetingID, MeetingTitle: r.MeetingTitle, Project: r.MeetingProject})
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		a, b := out.Items[i].Due, out.Items[j].Due
		return a != "" && (b == "" || a < b)
	})
	return nil, out, nil
}

type Projects struct {
	Projects []tools.Project `json:"projects"`
}

func (h *handlers) listProjects(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, Projects, error) {
	ps, err := h.surface.ListProjects()
	if err != nil {
		return nil, Projects{}, err
	}
	if ps == nil {
		ps = []tools.Project{}
	}
	return nil, Projects{Projects: ps}, nil
}

type NoteIn struct {
	ID   string `json:"id" jsonschema:"the meeting id"`
	Note string `json:"note" jsonschema:"the note to append"`
}

type Done struct {
	OK bool `json:"ok"`
}

func (h *handlers) addNote(_ context.Context, _ *mcp.CallToolRequest, in NoteIn) (*mcp.CallToolResult, Done, error) {
	if strings.TrimSpace(in.Note) == "" {
		return nil, Done{}, fmt.Errorf("note is empty")
	}
	if err := h.surface.AddNote(in.ID, in.Note); err != nil {
		return nil, Done{}, notFound(in.ID, err)
	}
	return nil, Done{OK: true}, nil
}

type RenameIn struct {
	ID    string `json:"id" jsonschema:"the meeting id"`
	Title string `json:"title" jsonschema:"the new title"`
}

func (h *handlers) renameMeeting(_ context.Context, _ *mcp.CallToolRequest, in RenameIn) (*mcp.CallToolResult, Done, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, Done{}, fmt.Errorf("title is empty")
	}
	if err := h.surface.RenameMeeting(in.ID, in.Title); err != nil {
		return nil, Done{}, notFound(in.ID, err)
	}
	return nil, Done{OK: true}, nil
}

type TagsIn struct {
	ID   string   `json:"id" jsonschema:"the meeting id"`
	Tags []string `json:"tags" jsonschema:"the meeting's new tags (replaces the current ones; the project is kept)"`
}

type SpeakerNamesIn struct {
	ID    string            `json:"id" jsonschema:"the meeting id"`
	Names map[string]string `json:"names" jsonschema:"speaker key → name; an empty name removes it"`
}

func (h *handlers) setSpeakerNames(_ context.Context, _ *mcp.CallToolRequest, in SpeakerNamesIn) (*mcp.CallToolResult, Done, error) {
	if len(in.Names) == 0 {
		return nil, Done{}, fmt.Errorf("names is required")
	}
	if err := h.surface.SetSpeakerNames(in.ID, in.Names); err != nil {
		return nil, Done{}, notFound(in.ID, err)
	}
	return nil, Done{OK: true}, nil
}

type GuestsIn struct {
	ID     string          `json:"id" jsonschema:"the meeting id"`
	Guests []meeting.Guest `json:"guests,omitempty" jsonschema:"the guests, each with a name, an email or both"`
	Text   string          `json:"text,omitempty" jsonschema:"or: a guest list pasted from a calendar"`
}

func (h *handlers) setGuests(_ context.Context, _ *mcp.CallToolRequest, in GuestsIn) (*mcp.CallToolResult, Done, error) {
	guests := append(in.Guests, meeting.ParseGuests(in.Text)...)
	if err := h.surface.SetGuests(in.ID, guests); err != nil {
		return nil, Done{}, notFound(in.ID, err)
	}
	return nil, Done{OK: true}, nil
}

func (h *handlers) setTags(_ context.Context, _ *mcp.CallToolRequest, in TagsIn) (*mcp.CallToolResult, Done, error) {
	m, err := h.surface.GetMeeting(in.ID)
	if err != nil {
		return nil, Done{}, notFound(in.ID, err)
	}
	tags := meeting.WithProject(plainTags(in.Tags), meeting.ProjectOf(m.Tags))
	if err := h.surface.SetMeetingTags(in.ID, tags); err != nil {
		return nil, Done{}, err
	}
	return nil, Done{OK: true}, nil
}
