package capture

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/gen2brain/malgo"

	"github.com/spacingmind/catchm/internal/audio"
	"github.com/spacingmind/catchm/internal/stt"
)

// SystemAudioSupported reports whether system-audio capture works on
// this platform. Windows: yes, via miniaudio's WASAPI loopback on the
// default render device. Linux: no — miniaudio has no loopback device
// type there, and detecting "the default sink's .monitor capture
// device" reliably across PulseAudio/PipeWire/ALSA is not something we
// can promise; a user who wants system audio on Linux can select a
// monitor device as their mic device today (M4's device list already
// enumerates capture devices, which includes monitors under
// PulseAudio). macOS: no — would need ScreenCaptureKit (out of scope,
// M19 spec).
func SystemAudioSupported() (bool, string) {
	switch runtime.GOOS {
	case "windows":
		return true, ""
	case "linux":
		return false, "system audio isn't supported on Linux with miniaudio; select your sound server's monitor device as the microphone instead"
	case "darwin":
		return false, "system audio isn't supported on macOS"
	default:
		return false, fmt.Sprintf("system audio isn't supported on %s", runtime.GOOS)
	}
}

// SystemSource captures whatever the default playback device is
// playing (WASAPI loopback, Windows only) as 16 kHz mono s16 PCM.
// miniaudio performs format conversion and resampling internally.
type SystemSource struct {
	ctx *malgo.AllocatedContext
	dev *malgo.Device
	ch  chan stt.PCM
}

// NewSystemSource opens the default playback device in loopback mode.
// During system silence WASAPI delivers nothing, so Chunks may go
// quiet; the Mixer pads those gaps with zeros to keep the stream
// real-time.
func NewSystemSource() (*SystemSource, error) {
	if ok, reason := SystemAudioSupported(); !ok {
		return nil, errors.New(reason)
	}

	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}

	// Loopback captures a playback device; the config's Playback
	// section describes its format. One channel: if the device mixes
	// stereo down to mono we lose nothing a meeting recording needs.
	cfg := malgo.DefaultDeviceConfig(malgo.Loopback)
	cfg.Playback.Format = malgo.FormatS16
	cfg.Playback.Channels = audio.Channels
	cfg.SampleRate = audio.SampleRate

	ch := make(chan stt.PCM, 8)
	callbacks := malgo.DeviceCallbacks{
		Data: func(_, in []byte, _ uint32) {
			if len(in) == 0 {
				return // WASAPI SILENT flag → nothing delivered
			}
			buf := make([]byte, len(in))
			copy(buf, in)
			select {
			case ch <- stt.PCM(buf):
			default: // consumer behind; drop rather than block the audio thread
			}
		},
	}

	dev, err := malgo.InitDevice(mctx.Context, cfg, callbacks)
	if err != nil {
		mctx.Free()
		return nil, fmt.Errorf("open system audio (loopback): %w", err)
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		mctx.Free()
		return nil, fmt.Errorf("start system audio: %w", err)
	}

	return &SystemSource{ctx: mctx, dev: dev, ch: ch}, nil
}

func (s *SystemSource) Chunks() <-chan stt.PCM { return s.ch }

func (s *SystemSource) Close() error {
	s.dev.Uninit()
	s.ctx.Free()
	close(s.ch)
	return nil
}
