package meeting

import (
	"reflect"
	"testing"
	"time"
)

func TestSpeakerKey(t *testing.T) {
	cases := map[Utterance]string{
		{Speaker: "1"}:                "1",
		{Speaker: "spk_2"}:            "2",
		{Speaker: "Speaker 3"}:        "3",
		{Speaker: "S4"}:               "4",
		{Speaker: "Speaker 03"}:       "3",
		{Speaker: "Alice"}:            "Alice",
		{Speaker: "2", Source: "mic"}: "2", // a label wins over the source
		{Source: "mic"}:               SpeakerMe,
		{Source: "system"}:            SpeakerOthers,
		{}:                            "",
	}
	for u, want := range cases {
		if got := SpeakerKey(u); got != want {
			t.Errorf("SpeakerKey(%+v) = %q, want %q", u, got, want)
		}
	}
}

func TestNamer(t *testing.T) {
	m := &Meeting{Speakers: map[string]string{"2": "Bao Tran", SpeakerOthers: "Acme team"}}
	n := m.Namer("An Nguyen")
	for key, want := range map[string]string{
		"1": "Speaker 1", "2": "Bao Tran", SpeakerMe: "An Nguyen", SpeakerOthers: "Acme team", "Alice": "Alice", "": "",
	} {
		if got := n.Label(key); got != want {
			t.Errorf("Label(%q) = %q, want %q", key, got, want)
		}
	}
	if got := (&Meeting{}).Namer("").Label(SpeakerMe); got != "Me" {
		t.Errorf("unnamed me = %q", got)
	}
}

func TestSpeakerStats(t *testing.T) {
	us := []Utterance{
		{Seq: 1, Speaker: "1", Source: "mic", Text: "hi"},
		{Seq: 2, Speaker: "2", Source: "system", Text: "hello"},
		{Seq: 3, Speaker: "spk_1", Source: "mic", Text: "so"},
		{Seq: 4, Speaker: "1", Text: "  "},
		{Seq: 5, Text: "no label"},
	}
	got := SpeakerStats(us)
	want := []SpeakerStat{{Key: "1", Lines: 2, Mic: 2, First: 1}, {Key: "2", Lines: 1, System: 1, First: 2}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stats = %+v", got)
	}
}

func TestParseGuests(t *testing.T) {
	cases := []struct {
		name, in string
		want     []Guest
	}{
		{"google calendar", `An Nguyen <an.nguyen@acme.com>, "Tran, Bao" <Bao@Acme.com>, carol@x.io`, []Guest{
			{Name: "An Nguyen", Email: "an.nguyen@acme.com"},
			{Name: "Tran, Bao", Email: "bao@acme.com"},
			{Name: "Carol", Email: "carol@x.io"},
		}},
		{"outlook", "Nguyen, An <an@acme.com>; Tran, Bao <bao@acme.com>", []Guest{
			{Name: "Nguyen, An", Email: "an@acme.com"},
			{Name: "Tran, Bao", Email: "bao@acme.com"},
		}},
		{"calendar panel: name line then email line", "An Nguyen\nan@acme.com\nOrganizer\nBao Tran (optional)\nbao@acme.com\ncarol@x.io\n", []Guest{
			{Name: "An Nguyen", Email: "an@acme.com"},
			{Name: "Bao Tran", Email: "bao@acme.com"},
			{Name: "Carol", Email: "carol@x.io"},
		}},
		{"duplicates merge by email", "an@acme.com\nAn Nguyen <AN@acme.com>", []Guest{{Name: "An", Email: "an@acme.com"}}},
		{"role and numeric addresses get no name", "team@acme.com, j2024@acme.com", []Guest{{Email: "team@acme.com"}, {Email: "j2024@acme.com"}}},
		{"names only", "An Nguyen, Bao Tran", []Guest{{Name: "An Nguyen"}, {Name: "Bao Tran"}}},
		{"blank", " \n ; , ", nil},
	}
	for _, c := range cases {
		if got := ParseGuests(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v\nwant %+v", c.name, got, c.want)
		}
	}
}

func TestStoreSpeakersAndGuests(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Start("Sync", time.Now(), StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSpeakerNames(m.ID, map[string]string{"1": "  An   Nguyen ", "2": "Bao"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSpeakerNames(m.ID, map[string]string{"2": ""}); err != nil { // un-name 2, keep 1
		t.Fatal(err)
	}
	if err := s.SetGuests(m.ID, []Guest{{Name: "An", Email: "An@Acme.com"}, {Email: "not an email"}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Speakers, map[string]string{"1": "An Nguyen"}) {
		t.Errorf("speakers = %v", got.Speakers)
	}
	if !reflect.DeepEqual(got.Guests, []Guest{{Name: "An", Email: "an@acme.com"}}) {
		t.Errorf("guests = %+v", got.Guests)
	}
	long := make([]byte, MaxSpeakerName+1)
	for i := range long {
		long[i] = 'a'
	}
	if err := s.SetSpeakerNames(m.ID, map[string]string{"1": string(long)}); err == nil {
		t.Error("an over-long name was accepted")
	}
}

func TestAutoMe(t *testing.T) {
	mic := func(k string, n int) []Utterance {
		var us []Utterance
		for i := 0; i < n; i++ {
			us = append(us, Utterance{Speaker: k, Source: "mic", Text: "x"})
		}
		return us
	}
	sys := Utterance{Speaker: "2", Source: "system", Text: "y"}
	cases := []struct {
		name  string
		us    []Utterance
		names map[string]string
		want  string
	}{
		{"one mic-heavy speaker", append(mic("1", 4), sys, sys, sys), nil, "1"},
		{"too few lines", append(mic("1", 2), sys), nil, ""},
		{"two share the mic", append(mic("1", 3), mic("3", 3)...), nil, ""},
		{"already named", mic("1", 5), map[string]string{"1": "Bao"}, ""},
		{"mostly system", append(append(mic("1", 2), Utterance{Speaker: "1", Source: "system", Text: "z"}), Utterance{Speaker: "1", Text: "w"}), nil, ""},
	}
	for _, c := range cases {
		if got := AutoMeKey(SpeakerStats(c.us), c.names); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}

	us := append(mic("1", 4), sys)
	n := (&Meeting{}).Namer("Alex").WithTranscript(us)
	if n.Label("1") != "Alex" || !n.IsMe("1") || n.IsMe("2") {
		t.Errorf("auto me: label %q", n.Label("1"))
	}
	// The user's own pick wins over the microphone's guess.
	n = (&Meeting{Me: "2"}).Namer("Alex").WithTranscript(us)
	if n.Label("2") != "Alex" || n.Label("1") != "Speaker 1" {
		t.Errorf("picked me: %q / %q", n.Label("2"), n.Label("1"))
	}
}

func TestSetMeSpeaker(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Start("Sync", time.Now(), StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSpeakerNames(m.ID, map[string]string{"2": "typo name", "3": "Carol"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeSpeaker(m.ID, "2"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(m.ID)
	if got.Me != "2" || !reflect.DeepEqual(got.Speakers, map[string]string{"3": "Carol"}) {
		t.Errorf("me = %q, speakers = %v", got.Me, got.Speakers)
	}
	if err := s.SetMeSpeaker(m.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Get(m.ID); got.Me != "" {
		t.Errorf("forgotten me = %q", got.Me)
	}
	if !(Self{Emails: []string{"a@b.co"}}).IsSelfEmail("A@B.co") {
		t.Error("self email not matched case-insensitively")
	}
}
