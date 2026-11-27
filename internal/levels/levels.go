// Package levels keeps a meeting's input-loudness timeline (levels.jsonl
// next to audio.wav) and uses it to tell which input carried an
// utterance: the microphone (the user) or computer audio (the other side
// of the call). See docs/plans/active/speaker-identity.md.
package levels

import (
	"bufio"
	"encoding/json"
	"errors"
	"math"
	"os"
	"sync"
	"time"

	"github.com/spacingmind/catchm/internal/audio"
	"github.com/spacingmind/catchm/internal/capture"
)

// File is the timeline's name in the meeting folder.
const File = "levels.jsonl"

// Step is one row's span: 5 mixer ticks.
const Step = 100 * time.Millisecond

const ticksPerRow = int(Step / (time.Second * capture.TickSamples / audio.SampleRate))

// Sources an utterance can be attributed to.
const (
	Mic    = "mic"
	System = "system"
)

// Ratio is how much louder (RMS) one input must be than the other over
// an utterance to win it. Below it (overlap, echo) there's no source.
const Ratio = 2.0

// minLevel is the RMS under which both inputs count as silent.
const minLevel = 0.002

// Row is one Step of the timeline. Mic/System are nil while that input
// isn't wired (a mic-only meeting has no System).
type Row struct {
	T      int64    `json:"t"` // ms from the start of the audio
	Mic    *float64 `json:"m,omitempty"`
	System *float64 `json:"s,omitempty"`
}

// Recorder collects mixer ticks into rows, writes them to the timeline
// file and keeps them for live attribution.
type Recorder struct {
	mu   sync.Mutex
	f    *os.File
	w    *bufio.Writer
	rows []Row

	// the row being built
	row        int64 // index of the row being built, -1 before the first tick
	n          int
	mic, sys   float64 // sums of squares
	micN, sysN int
}

// Create starts a timeline at path (replacing any old one).
func Create(path string) (*Recorder, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	return &Recorder{f: f, w: bufio.NewWriterSize(f, 16<<10), row: -1}, nil
}

// Observe is a capture.TickObserver.
func (r *Recorder) Observe(index uint64, lv capture.TickLevels) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := int64(index) / int64(ticksPerRow)
	if row != r.row {
		r.flushLocked()
		r.row = row
	}
	r.n++
	if lv.Mic >= 0 {
		r.mic += lv.Mic * lv.Mic
		r.micN++
	}
	if lv.System >= 0 {
		r.sys += lv.System * lv.System
		r.sysN++
	}
}

func (r *Recorder) flushLocked() {
	if r.row < 0 || r.n == 0 {
		return
	}
	row := Row{T: r.row * Step.Milliseconds()}
	if r.micN > 0 {
		row.Mic = rounded(math.Sqrt(r.mic / float64(r.micN)))
	}
	if r.sysN > 0 {
		row.System = rounded(math.Sqrt(r.sys / float64(r.sysN)))
	}
	r.rows = append(r.rows, row)
	if r.w != nil {
		b, _ := json.Marshal(row)
		_, _ = r.w.Write(append(b, '\n'))
	}
	r.n, r.mic, r.sys, r.micN, r.sysN = 0, 0, 0, 0, 0
}

func rounded(v float64) *float64 {
	v = math.Round(v*1e5) / 1e5
	return &v
}

// Source attributes the audio span [start, end) using the rows so far.
func (r *Recorder) Source(start, end time.Duration) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Attribute(r.rows, start, end)
}

// Close writes the last row and closes the file.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushLocked()
	if r.f == nil {
		return nil
	}
	err := r.w.Flush()
	if cerr := r.f.Close(); err == nil {
		err = cerr
	}
	r.f, r.w = nil, nil
	return err
}

// Read loads a timeline. A missing file is (nil, nil): meetings
// recorded before the timeline existed, or imported audio.
func Read(path string) ([]Row, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []Row
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var row Row
		if json.Unmarshal(sc.Bytes(), &row) == nil {
			rows = append(rows, row)
		}
	}
	return rows, sc.Err()
}

// Attribute names the input that carried the span [start, end): Mic or
// System when one is clearly louder (Ratio) over it, "" when both are
// wired but neither wins, when only one input is wired (a mic in a room
// hears everyone), or when the timeline doesn't cover the span. rows
// must be in time order.
func Attribute(rows []Row, start, end time.Duration) string {
	if end <= start || len(rows) == 0 {
		return ""
	}
	s, e := start.Milliseconds(), end.Milliseconds()
	stepMS := Step.Milliseconds()
	// first row overlapping the span
	lo, hi := 0, len(rows)
	for lo < hi {
		mid := (lo + hi) / 2
		if rows[mid].T+stepMS <= s {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	var mic, sys float64
	n := 0
	for i := lo; i < len(rows) && rows[i].T < e; i++ {
		row := rows[i]
		if row.Mic == nil || row.System == nil {
			return ""
		}
		mic += *row.Mic * *row.Mic
		sys += *row.System * *row.System
		n++
	}
	// Mostly uncovered (rows lost while the engine connected, or a span
	// past the end): don't guess.
	if n == 0 || int64(n)*stepMS*2 < e-s {
		return ""
	}
	micRMS, sysRMS := math.Sqrt(mic/float64(n)), math.Sqrt(sys/float64(n))
	switch {
	case micRMS < minLevel && sysRMS < minLevel:
		return ""
	case micRMS >= Ratio*sysRMS:
		return Mic
	case sysRMS >= Ratio*micRMS:
		return System
	}
	return ""
}
