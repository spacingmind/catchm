// Package capture provides audio capture sources that feed PCM chunks to
// an stt.StreamEngine: a real microphone (via malgo/miniaudio) and, for
// tests and offline replay, a WAV file source.
package capture

import (
	"encoding/hex"

	"github.com/gen2brain/malgo"

	"github.com/spacingmind/catchm/internal/audio"
	"github.com/spacingmind/catchm/internal/stt"
)

// Source produces PCM chunks until Close is called, at which point its
// Chunks channel is closed.
type Source interface {
	Chunks() <-chan stt.PCM
	Close() error
}

// Device describes one capture-capable audio device.
type Device struct {
	ID        string
	Name      string
	IsDefault bool
}

// Devices enumerates capture (input) devices, so a system-audio virtual
// device (e.g. BlackHole, VB-Cable) can be selected the same way a
// physical microphone is.
func Devices() ([]Device, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}
	defer ctx.Free()

	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, err
	}
	out := make([]Device, len(infos))
	for i, info := range infos {
		out[i] = Device{ID: info.ID.String(), Name: info.Name(), IsDefault: info.IsDefault != 0}
	}
	return out, nil
}

// MicSource captures 16kHz mono PCM from a microphone (or other capture
// device) using malgo/miniaudio.
type MicSource struct {
	ctx *malgo.AllocatedContext
	dev *malgo.Device
	ch  chan stt.PCM
}

// NewMicSource opens deviceID (its hex Device.ID, or "" for the system
// default) and starts capturing.
func NewMicSource(deviceID string) (*MicSource, error) {
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = audio.Channels
	cfg.SampleRate = audio.SampleRate
	if id, err := parseDeviceID(deviceID); err != nil {
		mctx.Free()
		return nil, err
	} else if id != nil {
		cfg.Capture.DeviceID = id.Pointer()
	}

	ch := make(chan stt.PCM, 8)
	callbacks := malgo.DeviceCallbacks{
		Data: func(_, in []byte, _ uint32) {
			buf := make([]byte, len(in))
			copy(buf, in)
			select {
			case ch <- stt.PCM(buf):
			default: // consumer is behind; drop rather than block the audio thread
			}
		},
	}

	dev, err := malgo.InitDevice(mctx.Context, cfg, callbacks)
	if err != nil {
		mctx.Free()
		return nil, err
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		mctx.Free()
		return nil, err
	}

	return &MicSource{ctx: mctx, dev: dev, ch: ch}, nil
}

func (m *MicSource) Chunks() <-chan stt.PCM { return m.ch }

func (m *MicSource) Close() error {
	m.dev.Uninit()
	m.ctx.Free()
	close(m.ch)
	return nil
}

func parseDeviceID(s string) (*malgo.DeviceID, error) {
	if s == "" {
		return nil, nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	var id malgo.DeviceID
	copy(id[:], b)
	return &id, nil
}
