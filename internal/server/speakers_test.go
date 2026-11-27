package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/meeting"
)

func TestMeetingSpeakersAndGuests(t *testing.T) {
	s, store := newTestServer(t)
	s.cfg.Profile.Name = "An Nguyen"
	m, err := store.Start("Sync", time.Now(), meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []meeting.Utterance{
		{Speaker: "1", Source: "mic", Text: "hi"},
		{Speaker: "2", Source: "system", Text: "hello"},
		{Speaker: "1", Text: "so"},
	} {
		if _, err := store.Append(m.ID, u); err != nil {
			t.Fatal(err)
		}
	}

	if w := do(t, s, "PATCH", "/api/meetings/"+m.ID, `{"speakers":{"2":"Bao Tran"},"guests":[{"name":"Bao Tran","email":"Bao@Acme.com"}]}`); w.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", w.Code, w.Body.String())
	}
	var got struct {
		Speakers []struct {
			Key   string `json:"key"`
			Name  string `json:"name"`
			Lines int    `json:"lines"`
			Mic   int    `json:"mic"`
		} `json:"speakers"`
		Me     string          `json:"me"`
		Guests []meeting.Guest `json:"guests"`
	}
	decode(t, do(t, s, "GET", "/api/meetings/"+m.ID+"/speakers", ""), &got)
	if got.Me != "An Nguyen" || len(got.Speakers) != 2 || got.Speakers[0].Key != "1" || got.Speakers[0].Lines != 2 || got.Speakers[0].Mic != 1 || got.Speakers[0].Name != "" || got.Speakers[1].Name != "Bao Tran" {
		t.Errorf("speakers = %+v", got)
	}
	if len(got.Guests) != 1 || got.Guests[0].Email != "bao@acme.com" {
		t.Errorf("guests = %+v", got.Guests)
	}

	if w := do(t, s, "PATCH", "/api/meetings/"+m.ID, `{"speakers":{"1":""}}`); w.Code != http.StatusOK {
		t.Errorf("clearing a name: %d", w.Code)
	}
	long := ""
	for i := 0; i <= meeting.MaxSpeakerName; i++ {
		long += "a"
	}
	if w := do(t, s, "PATCH", "/api/meetings/"+m.ID, `{"speakers":{"1":"`+long+`"}}`); w.Code != http.StatusBadRequest {
		t.Errorf("long name: %d", w.Code)
	}
	if w := do(t, s, "GET", "/api/meetings/nope/speakers", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown meeting: %d", w.Code)
	}
}

func TestParseGuestsEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	var got struct {
		Guests []meeting.Guest `json:"guests"`
	}
	decode(t, do(t, s, "POST", "/api/guests/parse", `{"text":"An Nguyen <an@acme.com>, bao.tran@acme.com"}`), &got)
	if len(got.Guests) != 2 || got.Guests[1].Name != "Bao Tran" {
		t.Errorf("guests = %+v", got.Guests)
	}
}

func TestMeetingMeAndProfile(t *testing.T) {
	t.Setenv("CATCHM_HOME", t.TempDir())
	s, store := newTestServer(t)
	if w := do(t, s, "PUT", "/api/config", `{"profile":{"name":"An","emails":[" An@Acme.com ","an@acme.com"],"aliases":["An","anh An"," "]}}`); w.Code != http.StatusOK {
		t.Fatalf("profile: %d %s", w.Code, w.Body.String())
	}
	var cfg struct {
		Profile struct {
			Emails  []string `json:"emails"`
			Aliases []string `json:"aliases"`
		} `json:"profile"`
	}
	decode(t, do(t, s, "GET", "/api/config", ""), &cfg)
	if len(cfg.Profile.Emails) != 1 || cfg.Profile.Emails[0] != "an@acme.com" || len(cfg.Profile.Aliases) != 2 {
		t.Errorf("profile = %+v", cfg.Profile)
	}
	if w := do(t, s, "PUT", "/api/config", `{"profile":{"emails":["not an email"]}}`); w.Code != http.StatusBadRequest {
		t.Errorf("bad email: %d", w.Code)
	}

	m, err := store.Start("Sync", time.Now(), meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		_, _ = store.Append(m.ID, meeting.Utterance{Speaker: "1", Source: "mic", Text: "mine"})
		_, _ = store.Append(m.ID, meeting.Utterance{Speaker: "2", Source: "system", Text: "theirs"})
	}
	var sp struct {
		Me     string `json:"me"`
		MeKey  string `json:"me_key"`
		AutoMe string `json:"auto_me"`
	}
	decode(t, do(t, s, "GET", "/api/meetings/"+m.ID+"/speakers", ""), &sp)
	if sp.Me != "An" || sp.AutoMe != "1" || sp.MeKey != "" {
		t.Errorf("speakers = %+v", sp)
	}
	if w := do(t, s, "PATCH", "/api/meetings/"+m.ID, `{"me":"2"}`); w.Code != http.StatusOK {
		t.Fatalf("me: %d", w.Code)
	}
	decode(t, do(t, s, "GET", "/api/meetings/"+m.ID+"/speakers", ""), &sp)
	if sp.MeKey != "2" || sp.AutoMe != "" {
		t.Errorf("after picking = %+v", sp)
	}
}
