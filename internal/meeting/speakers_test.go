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
