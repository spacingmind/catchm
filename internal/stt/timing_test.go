package stt

import (
	"testing"
	"time"
)

// Each engine turns its own word/token timing into Result.Start/End:
// audio offsets from the first sample sent (speaker-identity plan).

func TestDeepgramTiming(t *testing.T) {
	sp := func(i int) *int { return &i }
	words := []deepgramWord{
		{Word: "hello", Start: 0.5, End: 0.9, Speaker: sp(0)},
		{Word: "there", Start: 1.0, End: 1.4, Speaker: sp(0)},
		{Word: "hi", Start: 2.0, End: 2.2, Speaker: sp(1)},
	}
	rs := deepgramUtterances(words, []string{"hello there hi"})
	if len(rs) != 2 {
		t.Fatalf("results = %+v", rs)
	}
	if rs[0].Start != 500*time.Millisecond || rs[0].End != 1400*time.Millisecond || rs[1].Start != 2*time.Second || rs[1].End != 2200*time.Millisecond {
		t.Errorf("spans = %v-%v, %v-%v", rs[0].Start, rs[0].End, rs[1].Start, rs[1].End)
	}
	// No speakers: one result spanning every word.
	plain := deepgramUtterances([]deepgramWord{{Word: "a", Start: 1, End: 1.5}, {Word: "b", Start: 2, End: 3}}, []string{"a b"})
	if len(plain) != 1 || plain[0].Start != time.Second || plain[0].End != 3*time.Second || !plain[0].Timed() {
		t.Errorf("plain = %+v", plain)
	}
}

func TestElevenLabsTiming(t *testing.T) {
	s0, s1 := "speaker_0", "speaker_1"
	words := []elevenLabsWord{
		{Text: "Xin", Type: "word", Start: 0.2, End: 0.4, SpeakerID: &s0},
		{Text: " ", Type: "spacing", Start: 0.4, End: 0.5},
		{Text: "chào.", Type: "word", Start: 0.5, End: 0.9, SpeakerID: &s0},
		{Text: " ", Type: "spacing", Start: 0.9, End: 1.2},
		{Text: "Dạ.", Type: "word", Start: 1.2, End: 1.6, SpeakerID: &s1},
	}
	rs := elevenLabsSpeakerRuns(words, map[string]string{})
	if len(rs) != 2 || rs[0].Start != 200*time.Millisecond || rs[0].End != 900*time.Millisecond || rs[1].Start != 1200*time.Millisecond || rs[1].End != 1600*time.Millisecond {
		t.Errorf("results = %+v", rs)
	}
}

func TestSonioxTiming(t *testing.T) {
	res := buildResult([]sonioxToken{
		{Text: "hello", IsFinal: true, Speaker: "1", TranslationStatus: "original", StartMS: 1200, EndMS: 1500},
		{Text: " world", IsFinal: true, Speaker: "1", TranslationStatus: "original", StartMS: 1550, EndMS: 1900},
		{Text: "xin chào", IsFinal: true, TranslationStatus: "translation"},
		{Text: " more", IsFinal: false, StartMS: 2000, EndMS: 2300},
	})
	if res.Start != 1200*time.Millisecond || res.End != 1900*time.Millisecond {
		t.Errorf("span = %v-%v", res.Start, res.End)
	}
	// Tokens without timing leave the result untimed.
	if r := buildResult([]sonioxToken{{Text: "hi", IsFinal: true}}); r.Timed() {
		t.Errorf("untimed tokens gave %v-%v", r.Start, r.End)
	}
}

func TestSecondsSpan(t *testing.T) {
	if s, e := secondsSpan(2, 1); s != 0 || e != 0 {
		t.Errorf("inverted span = %v-%v", s, e)
	}
	if s, e := secondsSpan(0, 0.25); s != 0 || e != 250*time.Millisecond {
		t.Errorf("span = %v-%v", s, e)
	}
}
