package capture

import (
	"testing"
	"time"
)

func TestSilenceWatch(t *testing.T) {
	t0 := time.Unix(1000, 0)

	w := newSilenceWatch(t0)
	w.observe(make([]byte, 640))
	if w.noSignal(t0.Add(NoSignalAfter - time.Millisecond)) {
		t.Fatal("noSignal before the threshold")
	}
	if !w.noSignal(t0.Add(NoSignalAfter)) {
		t.Fatal("only zeros for NoSignalAfter: want noSignal")
	}

	// One non-zero byte anywhere latches "heard" for good.
	buf := make([]byte, 640)
	buf[401] = 0x01
	w.observe(buf)
	w.observe(make([]byte, 640))
	if w.noSignal(t0.Add(time.Hour)) {
		t.Fatal("audio was heard: noSignal must stay false")
	}
}

// signalSource is a fakeSource that also implements SignalReporter.
type signalSource struct {
	*fakeSource
	noSignal bool
}

func (s *signalSource) NoSignal() bool { return s.noSignal }

func TestMixerStatsNoSignal(t *testing.T) {
	mic := newFakeSource()
	sys := &signalSource{fakeSource: newFakeSource(), noSignal: true}
	m, err := NewMixer(newFakeClock(), mic, sys)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	micStats, sysStats, _ := m.Stats()
	if !sysStats.NoSignal {
		t.Fatal("system source reports NoSignal: Stats must carry it")
	}
	if micStats.NoSignal {
		t.Fatal("mic source doesn't implement SignalReporter: NoSignal must be false")
	}
}
