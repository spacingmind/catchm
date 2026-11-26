//go:build darwin

package capture

/*
#cgo CFLAGS: -fobjc-arc -Werror=unguarded-availability-new
#cgo LDFLAGS: -framework CoreAudio -framework AudioToolbox -framework Foundation
#include <stdlib.h>
#include "tap_darwin.h"
*/
import "C"

import (
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/spacingmind/catchm/internal/stt"
)

// SystemAudioSupported reports whether system-audio capture works on
// this Mac: Core Audio process taps need macOS 14.2+ (M25). Older macOS
// gets false with a reason naming 14.2; mic capture still works there.
func SystemAudioSupported() (bool, string) {
	var reason [256]C.char
	if C.catchm_tap_supported(&reason[0], C.int(len(reason))) == 1 {
		return true, ""
	}
	return false, C.GoString(&reason[0])
}

// pollInterval matches the Mixer's 20 ms tick; maxFramesPerRead bounds
// one drain (200 ms of 16 kHz audio) so a stalled reader catches up in
// a few polls instead of one huge chunk.
const (
	pollInterval     = 20 * time.Millisecond
	maxFramesPerRead = 3200
)

// SystemSource captures everything the Mac is playing, except CatchM
// itself, as 16 kHz mono s16 PCM via a Core Audio process tap
// (tap_darwin.m). Same surface as the Windows source.
type SystemSource struct {
	tap       *C.catchm_tap
	ch        chan stt.PCM
	done      chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
	dropped   atomic.Uint64
	overruns  atomic.Uint64 // copied from the tap by pump: no C call after Close
	silence   *silenceWatch
}

// NewSystemSource starts the tap. On first use macOS asks for "System
// Audio Recording" permission; a denial isn't an error here — the tap
// delivers zeros and NoSignal turns true after NoSignalAfter.
func NewSystemSource() (*SystemSource, error) {
	if ok, reason := SystemAudioSupported(); !ok {
		return nil, errors.New(reason)
	}
	var errBuf [256]C.char
	tap := C.catchm_tap_start(&errBuf[0], C.int(len(errBuf)))
	if tap == nil {
		return nil, errors.New("start system audio: " + C.GoString(&errBuf[0]))
	}
	s := &SystemSource{
		tap:     tap,
		ch:      make(chan stt.PCM, 8),
		done:    make(chan struct{}),
		silence: newSilenceWatch(time.Now()),
	}
	s.wg.Add(1)
	go s.pump()
	return s, nil
}

// pump drains the tap every pollInterval and forwards chunks with the
// same non-blocking send + drop counter as the malgo sources. Only this
// goroutine calls catchm_tap_read, and Close waits for it before
// catchm_tap_stop frees the tap.
func (s *SystemSource) pump() {
	defer s.wg.Done()
	buf := (*[maxFramesPerRead]C.int16_t)(C.malloc(C.size_t(maxFramesPerRead * 2)))
	defer C.free(unsafe.Pointer(buf))
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
		}
		s.overruns.Store(uint64(C.catchm_tap_overruns(s.tap)))
		for {
			n := int(C.catchm_tap_read(s.tap, &buf[0], C.int(maxFramesPerRead)))
			if n <= 0 {
				break // nothing captured yet (or a converter hiccup: retry next tick)
			}
			pcm := C.GoBytes(unsafe.Pointer(&buf[0]), C.int(n*2))
			s.silence.observe(pcm)
			select {
			case s.ch <- stt.PCM(pcm):
			default:
				s.dropped.Add(1) // visible, never silent
			}
			if n < maxFramesPerRead {
				break
			}
		}
	}
}

// rebuild runs the device-switch path on demand (tests only; in
// production the tap's property listeners call it).
func (s *SystemSource) rebuild() { C.catchm_tap_rebuild(s.tap) }

// leftovers counts this process's taps and CatchM aggregate devices
// (tests only: proves Close leaves nothing behind, M25 criterion 5).
func leftovers() int { return int(C.catchm_tap_leftovers()) }

// DroppedChunks implements capture.DropCounter: chunks dropped on the
// Go side (full channel) plus IOProc buffers the tap's ring rejected.
func (s *SystemSource) DroppedChunks() uint64 {
	return s.dropped.Load() + s.overruns.Load()
}

// NoSignal implements capture.SignalReporter: only digital zeros for
// NoSignalAfter since start — usually a denied permission.
func (s *SystemSource) NoSignal() bool { return s.silence.noSignal(time.Now()) }

func (s *SystemSource) Chunks() <-chan stt.PCM { return s.ch }

// stopWait bounds how long Close waits for the Core Audio teardown.
// It normally takes a few ms, but while macOS's "System Audio
// Recording" prompt is still open, coreaudiod blocks every IOProc
// teardown until the user answers (seen on macOS 27.2: minutes). Ending
// a meeting must not hang on that.
const stopWait = 2 * time.Second

// Close tears the tap, aggregate device and listeners down. Idempotent.
// The reader goroutine is always gone when Close returns; the Core
// Audio teardown finishes in the background if it outlasts stopWait.
func (s *SystemSource) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.wg.Wait() // no catchm_tap_read in flight past this point
		stopped := make(chan struct{})
		go func() {
			C.catchm_tap_stop(s.tap)
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-time.After(stopWait):
			log.Printf("capture: system audio teardown still pending after %v (permission prompt open?); finishing in the background", stopWait)
		}
		close(s.ch)
	})
	return nil
}
