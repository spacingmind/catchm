package capture

import (
	"encoding/binary"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/spacingmind/catchm/internal/audio"
	"github.com/spacingmind/catchm/internal/stt"
)

// Mixer combines up to two input sources (mic, system) into ONE
// 16 kHz s16 mono stream that flows at real-time pace, so the
// downstream STT session never starves during system-side silence
// and audio.wav's duration tracks wall time.
//
// Design (M19 spec §2):
//   - Fixed tick of TickSamples samples (20 ms = 320 samples at
//     16 kHz). Every tick, pull what each input's jitter buffer holds,
//     zero-pad any shortfall, sum with int16 saturation, emit one PCM
//     chunk.
//   - Pacing comes from an injected clock: each tick waits until
//     tickInterval after the previous tick's start, so the stream
//     never runs fast (a burst would carry >1× real-time audio, which
//     is exactly what the my-translator reference got wrong).
//   - Each input has a bounded jitter buffer (BufferSamples). A
//     device whose clock runs ahead (drift) drops its oldest samples
//     rather than growing unbounded or adding latency.
//   - The device callback (feed) never blocks: one mutex-guarded
//     append with a trim to the bound. Drops during true overflow are
//     the price of bounded latency; padding during underflow keeps
//     the output cadence, which the STT session needs more than it
//     needs those specific samples.
//
// Buffer numbers: 20 ms tick, 400 ms bound (20 ticks) per input —
// generous against OS scheduling jitter, far below anything a live
// transcript would tolerate as latency (the bound is only hit under
// clock drift, where the alternative is unbounded growth).
const (
	TickSamples    = 320  // 20 ms at 16 kHz
	BufferSamples  = 6400 // 400 ms at 16 kHz
	tickIntervalNs = int64(TickSamples) * int64(time.Second) / int64(audio.SampleRate)
)

var tickInterval = time.Duration(tickIntervalNs)

// Clock is the mixer's time source; tests inject a fake.
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

// RealClock is the production Clock.
type RealClock struct{}

func (RealClock) Now() time.Time        { return time.Now() }
func (RealClock) Sleep(d time.Duration) { time.Sleep(d) }

// Levels is one input's level over the last consumed tick window,
// 0–1 scale relative to int16 full scale.
type Levels struct {
	RMS  float64
	Peak float64
}

// mixerInput is one source's state: jitter buffer, current levels.
type mixerInput struct {
	mu     sync.Mutex
	buf    []int16 // jitter buffer, bounded at BufferSamples
	rms    float64
	peak   float64
	active bool
}

// feed appends samples to the jitter buffer. Called from the audio
// device thread: never blocks, bounded work (copy + trim).
func (in *mixerInput) feed(p stt.PCM) {
	samples := bytesToSamples(p)
	in.mu.Lock()
	in.buf = append(in.buf, samples...)
	if len(in.buf) > BufferSamples {
		// Clock drift ahead: drop oldest, keep latency bounded.
		in.buf = in.buf[len(in.buf)-BufferSamples:]
	}
	in.mu.Unlock()
}

// takeWindow pulls up to n samples, zero-padding any shortfall, and
// records levels over the samples actually consumed (padding excluded,
// so a silent system input reports ~0, not "zeros counted as signal").
func (in *mixerInput) takeWindow(n int) []int16 {
	in.mu.Lock()
	defer in.mu.Unlock()
	take := n
	if take > len(in.buf) {
		take = len(in.buf)
	}
	lv := levelOf(in.buf[:take])
	in.rms, in.peak = lv.RMS, lv.Peak
	window := make([]int16, n)
	copy(window, in.buf[:take])
	in.buf = in.buf[take:]
	return window
}

// levelOf computes RMS and peak over samples (0–1 of full scale).
func levelOf(samples []int16) Levels {
	if len(samples) == 0 {
		return Levels{}
	}
	var sumSq float64
	peak := float64(0)
	for _, s := range samples {
		v := float64(s) / 32768
		sumSq += v * v
		if a := math.Abs(v); a > peak {
			peak = a
		}
	}
	return Levels{RMS: math.Sqrt(sumSq / float64(len(samples))), Peak: peak}
}

// Mixer implements Source over 1–2 inputs.
type Mixer struct {
	mic    *mixerInput
	system *mixerInput

	clock Clock
	out   chan stt.PCM
	done  chan struct{}
	wg    sync.WaitGroup

	mu     sync.Mutex // guards closed flag (inputs are static per Mixer; switching swaps the whole Mixer's inputs at the pipeline layer)
	closed bool
}

// NewMixer starts the tick loop. Either input may be nil; at least one
// must be present (a zero-input mixer would emit silence forever).
func NewMixer(clock Clock, mic, system Source) (*Mixer, error) {
	if mic == nil && system == nil {
		return nil, errors.New("mixer: no inputs")
	}
	m := &Mixer{
		mic:    &mixerInput{active: mic != nil},
		system: &mixerInput{active: system != nil},
		clock:  clock,
		out:    make(chan stt.PCM, 8),
		done:   make(chan struct{}),
	}
	if mic != nil {
		m.wg.Add(1)
		go m.pump(mic, m.mic)
	}
	if system != nil {
		m.wg.Add(1)
		go m.pump(system, m.system)
	}

	// Bounded warmup: wait (real time, not the injected clock — this is
	// a scheduling concern, not a pacing one) until some input has data,
	// so the first ticks carry real content when content exists. A live
	// mic delivers its first chunk well inside the window; a fully
	// silent source (e.g. system-only with nothing playing) times out
	// and the stream starts on padding, per spec.
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) && !m.anyInputBuffered() {
		time.Sleep(time.Millisecond)
	}

	m.wg.Add(1)
	go m.run()
	return m, nil
}

// anyInputBuffered reports whether any active input's jitter buffer
// holds at least one sample.
func (m *Mixer) anyInputBuffered() bool {
	for _, in := range []*mixerInput{m.mic, m.system} {
		if in != nil && in.active && in.bufferedLen() > 0 {
			return true
		}
	}
	return false
}

// pump moves chunks from a source into its jitter buffer until either
// the source closes or the mixer stops. Input sources are owned by the
// pipeline, so Close signals done rather than closing their channels.
func (m *Mixer) pump(src Source, in *mixerInput) {
	defer m.wg.Done()
	ch := src.Chunks()
	for {
		select {
		case <-m.done:
			return
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			in.feed(chunk)
		}
	}
}

// run is the tick loop: every tickInterval, mix one TickSamples chunk.
func (m *Mixer) run() {
	defer m.wg.Done()
	next := m.clock.Now()
	var mixed []int16
	for {
		next = next.Add(tickInterval)
		if d := next.Sub(m.clock.Now()); d > 0 {
			m.clock.Sleep(d)
		}
		select {
		case <-m.done:
			return
		default:
		}

		mixed = m.mixTick(mixed[:0])

		buf := make([]byte, TickSamples*2)
		for i, s := range mixed {
			binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
		}
		select {
		case m.out <- stt.PCM(buf):
		case <-m.done:
			return
		default: // downstream behind: drop the tick rather than stall pacing
		}
	}
}

// mixTick sums each active input's window (zero-padded) with int16
// saturation; inactive inputs contribute zeros.
func (m *Mixer) mixTick(dst []int16) []int16 {
	if cap(dst) < TickSamples {
		dst = make([]int16, 0, TickSamples)
	}
	dst = dst[:TickSamples]
	clear(dst)
	for _, in := range []*mixerInput{m.mic, m.system} {
		if in == nil || !in.active {
			continue
		}
		w := in.takeWindow(TickSamples)
		for i := 0; i < TickSamples; i++ {
			dst[i] = saturateAdd(dst[i], w[i])
		}
	}
	return dst
}

func saturateAdd(a, b int16) int16 {
	sum := int32(a) + int32(b)
	if sum > math.MaxInt16 {
		return math.MaxInt16
	}
	if sum < math.MinInt16 {
		return math.MinInt16
	}
	return int16(sum)
}

func bytesToSamples(p stt.PCM) []int16 {
	n := len(p) / 2
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(binary.LittleEndian.Uint16(p[i*2:]))
	}
	return out
}

// bufferedLen reports how many samples an input's jitter buffer holds
// (test synchronization: a test waits for pre-fed data to land before
// asserting on tick content).
func (in *mixerInput) bufferedLen() int {
	in.mu.Lock()
	defer in.mu.Unlock()
	return len(in.buf)
}

// Chunks implements Source.
func (m *Mixer) Chunks() <-chan stt.PCM { return m.out }

// Levels reports each active input's level over its last consumed
// tick window.
func (m *Mixer) Levels() (mic, system Levels) {
	for _, pair := range []struct {
		in  *mixerInput
		dst *Levels
	}{{m.mic, &mic}, {m.system, &system}} {
		pair.in.mu.Lock()
		*pair.dst = Levels{RMS: pair.in.rms, Peak: pair.in.peak}
		pair.in.mu.Unlock()
	}
	return mic, system
}

// Close stops the tick loop. Input sources are NOT closed here — the
// pipeline owns them (mid-session switching swaps inputs at that
// layer, keeping this Mixer's output channel alive).
func (m *Mixer) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()
	close(m.done)
	m.wg.Wait()
	close(m.out)
	return nil
}
