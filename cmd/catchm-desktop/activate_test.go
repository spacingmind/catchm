package main

import "testing"

func TestParseMeetingURI(t *testing.T) {
	cases := []struct {
		name string
		arg  string
		want string
	}{
		{"meeting uri", "catchm://meeting/04ca1a14b0554486", "04ca1a14b0554486"},
		{"uppercase hex id", "catchm://meeting/04CA1A14B0554486", "04CA1A14B0554486"},
		{"surrounding whitespace", " catchm://meeting/04ca1a14b0554486\t", "04ca1a14b0554486"},
		{"empty id", "catchm://meeting/", ""},
		{"other path", "catchm://settings", ""},
		{"other scheme", "https://meeting/04ca1a14b0554486", ""},
		{"not a uri", "C:\\Program Files\\CatchM\\catchm-desktop.exe", ""},
		{"path traversal id", "catchm://meeting/../etc/passwd", ""},
		{"non-hex id", "catchm://meeting/zz-not-hex", ""},
	}
	for _, tc := range cases {
		if got := parseMeetingURI(tc.arg); got != tc.want {
			t.Errorf("%s: parseMeetingURI(%q) = %q, want %q", tc.name, tc.arg, got, tc.want)
		}
	}
}

func TestMeetingArgFromArgs(t *testing.T) {
	args := []string{"catchm-desktop.exe", "--autostart", "catchm://meeting/04ca1a14b0554486"}
	if got := meetingArgFromArgs(args); got != "catchm://meeting/04ca1a14b0554486" {
		t.Fatalf("meetingArgFromArgs = %q, want the uri arg", got)
	}
	if got := meetingArgFromArgs([]string{"catchm-desktop.exe"}); got != "" {
		t.Fatalf("meetingArgFromArgs with no uri = %q, want \"\"", got)
	}
}
