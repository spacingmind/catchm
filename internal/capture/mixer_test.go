package capture

import (
	"encoding/binary"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/stt"
)

// fakeClock is an injectable Clock whose Sleep advances virtual time
// (no real sleeping) and wakes tick loops instantly.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(0, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Sleep advances virtual time by d AND yields the real scheduler for
// a tiny slice, so the tick loop (driven by virtual time) and the
// pumps/waitForData (driven by real time) interleave like they do in
// production. Without the real yield the tick loop spins through every
// virtual tick before any pump ever runs.
func (c *fakeClock) Sleep(d time.Duration) {
	if d <= 0 {
		return
	}
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
	time.Sleep(200 * time.Microsecond)
}

// fakeSource is a scriptable input: pre-loaded chunks, closed when
// drained (or held open).
type fakeSource struct {
	ch       chan stt.PCM
	closeNow bool // close the channel after the script drains
}

func newFakeSource(scripts ...[]int16) *fakeSource {
	ch := make(chan stt.PCM, len(scripts))
	for _, s := range scripts {
		ch <- samplesToPCM(s)
	}
	return &fakeSource{ch: ch}
}

func (f *fakeSource) Chunks() <-chan stt.PCM { return f.ch }

func (f *fakeSource) Close() error { close(f.ch); return nil }

func samplesToPCM(s []int16) stt.PCM {
	buf := make([]byte, len(s)*2)
	for i, v := range s {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(v))
	}
	return stt.PCM(buf)
}

func pcmToSamples(p stt.PCM) []int16 {
	return bytesToSamples(p)
}

// drainTicks reads n ticks from the mixer with a timeout so a pacing
// bug fails the test instead of hanging it.
func drainTicks(t *testing.T, m *Mixer, n int) [][]int16 {
	t.Helper()
	out := make([][]int16, 0, n)
	deadline := time.After(2 * time.Second)
	for len(out) < n {
		select {
		case chunk, ok := <-m.Chunks():
			if !ok {
				t.Fatalf("chunks closed after %d ticks, want %d", len(out), n)
			}
			if len(chunk) != TickSamples*2 {
				t.Fatalf("tick length = %d bytes, want %d", len(chunk), TickSamples*2)
			}
			out = append(out, pcmToSamples(chunk))
		case <-deadline:
			t.Fatalf("timed out after %d ticks, want %d (pacing stalled?)", len(out), n)
		}
	}
	return out
}

// waitForData polls (real time) until the input's jitter buffer holds
// at least n samples, so assertions on the FIRST tick are deterministic
// despite goroutine scheduling.
func waitForData(t *testing.T, m *Mixer, in *mixerInput, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if in.bufferedLen() >= n {
			return
		}
		select {
		case <-m.done:
			t.Fatal("mixer stopped while waiting for input data")
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("input data never arrived (want %d samples)", n)
		}
		time.Sleep(time.Millisecond)
	}
}

func constSamples(n int, v int16) []int16 {
	s := make([]int16, n)
	for i := range s {
		s[i] = v
	}
	return s
}

// Sum + saturation at the int16 bounds: two full-scale inputs must
// clamp at MaxInt16, not wrap negative.
func TestMixerSaturatesAtBounds(t *testing.T) {
	mic := newFakeSource(constSamples(TickSamples, 20000), constSamples(TickSamples, math.MaxInt16))
	sys := newFakeSource(constSamples(TickSamples, 20000), constSamples(TickSamples, math.MinInt16))
	m, err := NewMixer(newFakeClock(), mic, sys)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	waitForData(t, m, m.mic, TickSamples)
	waitForData(t, m, m.system, TickSamples)

	ticks := drainTicks(t, m, 2)
	for i, v := range ticks[0] {
		if v != math.MaxInt16 {
			t.Fatalf("tick0 sample %d = %d, want MaxInt16 (20000+20000 clamps)", i, v)
		}
	}
	// 32767 + (-32768) = -1: an in-range sum passes through unsaturated.
	for i, v := range ticks[1] {
		if v != -1 {
			t.Fatalf("tick1 sample %d = %d, want -1", i, v)
		}
	}
}

// Zero-padding on underflow: an input with nothing buffered
// contributes zeros; the OTHER input passes through unchanged.
func TestMixerZeroPadsUnderflow(t *testing.T) {
	// Mic has one tick of data; system has nothing.
	mic := newFakeSource(constSamples(TickSamples, 1000))
	m, err := NewMixer(newFakeClock(), mic, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	waitForData(t, m, m.mic, TickSamples)

	ticks := drainTicks(t, m, 3)
	for i, v := range ticks[0] {
		if v != 1000 {
			t.Fatalf("tick0 sample %d = %d, want 1000 (passthrough)", i, v)
		}
	}
	for ti := 1; ti < 3; ti++ {
		for i, v := range ticks[ti] {
			if v != 0 {
				t.Fatalf("tick%d sample %d = %d, want zero padding", ti, i, v)
			}
		}
	}
}

// One-input passthrough: samples emerge unchanged, split across ticks.
func TestMixerOneInputPassthrough(t *testing.T) {
	script := make([]int16, TickSamples*2)
	for i := range script {
		script[i] = int16(i + 1)
	}
	mic := newFakeSource(script)
	m, err := NewMixer(newFakeClock(), mic, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	waitForData(t, m, m.mic, 2*TickSamples)

	ticks := drainTicks(t, m, 2)
	for i, v := range ticks[0] {
		if v != int16(i+1) {
			t.Fatalf("tick0[%d] = %d, want %d", i, v, i+1)
		}
	}
	for i, v := range ticks[1] {
		if v != int16(TickSamples+i+1) {
			t.Fatalf("tick1[%d] = %d, want %d", i, v, TickSamples+i+1)
		}
	}
}

// Real output is the sample-wise sum.
func TestMixerSums(t *testing.T) {
	mic := newFakeSource(constSamples(TickSamples, 100))
	sys := newFakeSource(constSamples(TickSamples, 234))
	m, err := NewMixer(newFakeClock(), mic, sys)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	waitForData(t, m, m.mic, TickSamples)
	waitForData(t, m, m.system, TickSamples)

	ticks := drainTicks(t, m, 1)
	for i, v := range ticks[0] {
		if v != 334 {
			t.Fatalf("sample %d = %d, want 334", i, v)
		}
	}
}

// Overflow drops OLDEST samples and bounds the buffer: feed more than
// BufferSamples, then verify what comes out is the newest window.
func TestMixerInputBufferBoundDropsOldest(t *testing.T) {
	in := &mixerInput{active: true}
	script := make([]int16, BufferSamples+TickSamples)
	for i := range script {
		script[i] = int16(i)
	}
	// Feed in two chunks (as a device would).
	half := len(script) / 2
	in.feed(samplesToPCM(script[:half]))
	in.feed(samplesToPCM(script[half:]))

	if got := len(in.buf); got != BufferSamples {
		t.Fatalf("buffer len = %d, want bound %d", got, BufferSamples)
	}
	// The oldest (BufferSamples+TickSamples) - BufferSamples = one tick
	// were dropped; first taken sample must be script[TickSamples].
	w := in.takeWindow(TickSamples)
	if w[0] != int16(TickSamples) {
		t.Fatalf("first sample after overflow = %d, want %d", w[0], TickSamples)
	}
}

// Output cadence is real-time: N ticks → N×320 samples, and the
// virtual clock advanced exactly N tick intervals.
func TestMixerRealTimeCadence(t *testing.T) {
	clock := newFakeClock()
	mic := newFakeSource(constSamples(TickSamples*5, 500))
	m, err := NewMixer(clock, mic, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	waitForData(t, m, m.mic, 5*TickSamples)

	ticks := drainTicks(t, m, 5)
	total := 0
	for _, tk := range ticks {
		total += len(tk)
	}
	if total != 5*TickSamples {
		t.Fatalf("total samples = %d, want %d", total, 5*TickSamples)
	}
	// Virtual elapsed is at least 5 intervals (the loop may already be
	// sleeping toward tick 6) and at most one interval ahead — output
	// pace tracks real time and never runs fast.
	elapsed := clock.Now().Sub(time.Unix(0, 0))
	if elapsed < 5*tickInterval || elapsed > 6*tickInterval {
		t.Fatalf("virtual elapsed = %v, want within [%v, %v]", elapsed, 5*tickInterval, 6*tickInterval)
	}
}

// Levels: silence → 0; constant near-full-scale → known RMS/peak;
// padded (starved) input reports ~0 because padding is excluded.
func TestMixerLevels(t *testing.T) {
	mic := newFakeSource(constSamples(TickSamples, 16384)) // half scale
	sys := newFakeSource(constSamples(TickSamples, 0))     // explicit silence
	m, err := NewMixer(newFakeClock(), mic, sys)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	waitForData(t, m, m.mic, TickSamples)
	waitForData(t, m, m.system, TickSamples)

	drainTicks(t, m, 1)
	m.Close() // stop the loop so no padding tick overwrites the window
	gotMic, gotSys, _ := m.Stats()
	want := float64(16384) / 32768
	if math.Abs(gotMic.RMS-want) > 1e-9 || math.Abs(gotMic.Peak-want) > 1e-9 {
		t.Fatalf("mic levels = %+v, want rms=peak=%.4f", gotMic, want)
	}
	if gotSys.RMS != 0 || gotSys.Peak != 0 {
		t.Fatalf("system levels = %+v, want zeros", gotSys)
	}

	// A silent input reports zeros (padding never counts as signal).
	quiet := newFakeSource(constSamples(TickSamples, 0))
	qm, err := NewMixer(newFakeClock(), quiet, nil)
	if err != nil {
		t.Fatal(err)
	}
	drainTicks(t, qm, 1)
	qm.Close()
	gotMic, _, _ = qm.Stats()
	if gotMic.RMS != 0 || gotMic.Peak != 0 {
		t.Fatalf("silent-input levels = %+v, want zeros", gotMic)
	}
}

// Full-scale sine RMS: a ±32767 sine over a whole number of periods
// ≈ 0.7071 of full scale.
func TestMixerSineLevels(t *testing.T) {
	n := TickSamples // 320 samples; 16kHz/320 = 50Hz fundamental — whole periods
	script := make([]int16, n)
	for i := range script {
		script[i] = int16(32767 * math.Sin(2*math.Pi*float64(i)/float64(n)))
	}
	mic := newFakeSource(script)
	m, err := NewMixer(newFakeClock(), mic, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	waitForData(t, m, m.mic, TickSamples)

	drainTicks(t, m, 1)
	m.Close() // freeze at the sine window
	got, _, _ := m.Stats()
	if math.Abs(got.RMS-0.7071) > 0.001 {
		t.Fatalf("sine RMS = %.4f, want ≈0.7071", got.RMS)
	}
	if got.Peak < 0.999 {
		t.Fatalf("sine peak = %.4f, want ≈1.0", got.Peak)
	}
}

// NewMixer with no inputs is rejected up front.
func TestMixerRejectsNoInputs(t *testing.T) {
	if _, err := NewMixer(newFakeClock(), nil, nil); err == nil {
		t.Fatal("expected error for zero inputs")
	}
}

// A deliberately slow consumer (never reads) forces the output buffer
// full; the mixer must keep its pace and COUNT the dropped ticks rather
// than blocking or running silent.
func TestMixerCountsDroppedTicksUnderSlowConsumer(t *testing.T) {
	// Lots of data so the mixer never starves during the test.
	mic := newFakeSource(constSamples(TickSamples*200, 1000))
	m, err := NewMixer(newFakeClock(), mic, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// Do NOT read Chunks: after the 25-tick output buffer fills, every
	// further tick drops. Warmup already ensured data; let it churn.
	time.Sleep(100 * time.Millisecond)

	_, _, dropped := m.Stats()
	if dropped == 0 {
		t.Fatal("slow consumer produced zero dropped ticks — drops are silent again?")
	}
	// And the mixer never blocked: dropped grows while virtual time advances.
	before := dropped
	time.Sleep(50 * time.Millisecond)
	_, _, dropped = m.Stats()
	if dropped <= before {
		t.Fatalf("dropped ticks stopped growing (%d -> %d): tick loop stalled?", before, dropped)
	}
}

// Jitter-buffer overflow counts dropped samples per input.
func TestMixerCountsDroppedSamplesOnOverflow(t *testing.T) {
	in := &mixerInput{active: true}
	script := make([]int16, BufferSamples+TickSamples)
	for i := range script {
		script[i] = int16(i)
	}
	in.feed(samplesToPCM(script))
	if got := in.droppedSamples.Load(); got != uint64(TickSamples) {
		t.Fatalf("droppedSamples = %d, want %d", got, TickSamples)
	}
}

// SetInputs swaps inputs mid-stream: output cadence never gaps, the
// new input's content flows within a tick, and a failed protocol
// (caller never calls SetInputs) is a no-op on the mixer.
func TestMixerSetInputsKeepsCadence(t *testing.T) {
	micA := newFakeSource(constSamples(TickSamples*6, 1111))
	m, err := NewMixer(newFakeClock(), micA, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// Drain 2 ticks from the initial input.
	ticks := drainTicks(t, m, 2)

	// Swap mic -> system-only with fresh content.
	sysB := newFakeSource(constSamples(TickSamples*10, 2222))
	m.SetInputs(nil, sysB)

	// Cadence continues: more ticks arrive without gap (drainTicks
	// would time out if the loop stalled) and content flips to 2222
	// within a bounded number of ticks (fresh input starts empty, so at
	// most one padded tick + scheduling).
	ticks = append(ticks, drainTicks(t, m, 6)...)
	foundNew := false
	for ti := 2; ti < len(ticks) && !foundNew; ti++ {
		for _, v := range ticks[ti] {
			if v == 2222 {
				foundNew = true
				break
			}
		}
	}
	if !foundNew {
		t.Fatal("new input's content never flowed after swap")
	}
	// Every tick still full-length: no cadence gap.
	for ti, tk := range ticks {
		if len(tk) != TickSamples {
			t.Fatalf("tick %d length %d after swap — cadence gap", ti, len(tk))
		}
	}
}

// The failed-open protocol: caller opens new source, it fails, caller
// never calls SetInputs — the old input keeps flowing untouched.
func TestMixerFailedOpenKeepsOldInput(t *testing.T) {
	mic := newFakeSource(constSamples(TickSamples*6, 700))
	m, err := NewMixer(newFakeClock(), mic, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// "Open failed": simulate by simply not calling SetInputs.
	ticks := drainTicks(t, m, 5)
	for ti, tk := range ticks {
		for i, v := range tk {
			if v != 0 && v != 700 {
				t.Fatalf("tick %d sample %d = %d, want 700 or padding 0", ti, i, v)
			}
		}
	}
}
