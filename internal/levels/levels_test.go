package levels

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/capture"
)

// feed observes ticks [from, to) with the given levels.
func feed(r *Recorder, from, to int, mic, sys float64) {
	for i := from; i < to; i++ {
		r.Observe(uint64(i), capture.TickLevels{Mic: mic, System: sys})
	}
}

func TestRecorderAttributes(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	r, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// 0–1 s: the user talks; 1–2 s: the call; 2–3 s: both at once;
	// 3–4 s: silence.
	feed(r, 0, 50, 0.2, 0.01)
	feed(r, 50, 100, 0.03, 0.25)
	feed(r, 100, 150, 0.2, 0.18)
	feed(r, 150, 200, 0.0005, 0.0003)

	cases := []struct {
		start, end time.Duration
		want       string
	}{
		{0, time.Second, Mic},
		{1100 * time.Millisecond, 1900 * time.Millisecond, System},
		{2 * time.Second, 3 * time.Second, ""},
		{3 * time.Second, 4 * time.Second, ""},
		{9 * time.Second, 10 * time.Second, ""}, // past the timeline
		{time.Second, time.Second, ""},          // empty span
	}
	for _, c := range cases {
		if got := r.Source(c.start, c.end); got != c.want {
			t.Errorf("Source(%v, %v) = %q, want %q", c.start, c.end, got, c.want)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	rows, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 40 || rows[0].T != 0 || rows[39].T != 3900 {
		t.Fatalf("rows = %d, first %+v", len(rows), rows[0])
	}
	if got := Attribute(rows, 1100*time.Millisecond, 1900*time.Millisecond); got != System {
		t.Errorf("from the file = %q, want system", got)
	}
}

func TestMicOnlyHasNoSource(t *testing.T) {
	r, err := Create(filepath.Join(t.TempDir(), File))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// A mic alone hears everyone in the room: no attribution.
	feed(r, 0, 60, 0.3, -1)
	if got := r.Source(0, time.Second); got != "" {
		t.Errorf("mic-only = %q", got)
	}
}

func TestReadMissing(t *testing.T) {
	rows, err := Read(filepath.Join(t.TempDir(), File))
	if rows != nil || err != nil {
		t.Errorf("missing timeline = %v, %v", rows, err)
	}
}
