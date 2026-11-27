package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/stt"
)

// cloudEngine is one bring-your-own-key transcription provider
// (docs/plans/completed/cloud-mode.md). Every place that names engines —
// live start, refine/import, status, config validation, key checks —
// goes through this table, so a new provider is one entry here plus
// its stt package.
type cloudEngine struct {
	name   string // config.Engine value and meeting frontmatter engine
	label  string // shown in messages
	key    func(*config.Config) string
	stream func(*config.Config) stt.StreamEngine
	batch  func(*config.Config) stt.BatchEngine
	// verify checks a key with the provider's cheapest call; nil when
	// the provider has none wired yet.
	verify func(ctx context.Context, key string) error
}

var cloudEngines = []cloudEngine{
	{
		name: "soniox", label: "Soniox",
		key:    func(c *config.Config) string { return c.Soniox.APIKey },
		stream: func(c *config.Config) stt.StreamEngine { return stt.NewSonioxEngine(c.Soniox.URL, c.Soniox.APIKey) },
		batch:  func(c *config.Config) stt.BatchEngine { return stt.NewSonioxAsyncEngine(c.Soniox.APIKey, "") },
	},
	{
		name: "deepgram", label: "Deepgram",
		key:    func(c *config.Config) string { return c.Deepgram.APIKey },
		stream: func(c *config.Config) stt.StreamEngine { return stt.NewDeepgramEngine(c.Deepgram.APIKey) },
		batch:  func(c *config.Config) stt.BatchEngine { return stt.NewDeepgramEngine(c.Deepgram.APIKey) },
		verify: func(ctx context.Context, key string) error { return stt.NewDeepgramEngine(key).VerifyKey(ctx) },
	},
	{
		name: "elevenlabs", label: "ElevenLabs",
		key:    func(c *config.Config) string { return c.ElevenLabs.APIKey },
		stream: func(c *config.Config) stt.StreamEngine { return stt.NewElevenLabsEngine(c.ElevenLabs.APIKey) },
		batch:  func(c *config.Config) stt.BatchEngine { return stt.NewElevenLabsEngine(c.ElevenLabs.APIKey) },
		verify: func(ctx context.Context, key string) error { return stt.NewElevenLabsEngine(key).VerifyKey(ctx) },
	},
	{
		name: "speechmatics", label: "Speechmatics",
		key:    func(c *config.Config) string { return c.Speechmatics.APIKey },
		stream: func(c *config.Config) stt.StreamEngine { return stt.NewSpeechmaticsEngine(c.Speechmatics.APIKey) },
		batch:  func(c *config.Config) stt.BatchEngine { return stt.NewSpeechmaticsEngine(c.Speechmatics.APIKey) },
		verify: func(ctx context.Context, key string) error { return stt.NewSpeechmaticsEngine(key).VerifyKey(ctx) },
	},
	{
		name: "gladia", label: "Gladia",
		key:    func(c *config.Config) string { return c.Gladia.APIKey },
		stream: func(c *config.Config) stt.StreamEngine { return stt.NewGladiaEngine(c.Gladia.APIKey) },
		batch:  func(c *config.Config) stt.BatchEngine { return stt.NewGladiaEngine(c.Gladia.APIKey) },
		verify: func(ctx context.Context, key string) error { return stt.NewGladiaEngine(key).VerifyKey(ctx) },
	},
}

func findCloudEngine(name string) (cloudEngine, bool) {
	for _, e := range cloudEngines {
		if e.name == name {
			return e, true
		}
	}
	return cloudEngine{}, false
}

// validEngine: whisper or a registered cloud engine.
func validEngine(name string) bool {
	_, ok := findCloudEngine(name)
	return ok || name == "whisper"
}

// missingKeyError is the typed cause for a cloud engine without its key,
// so capture errors get CaptureErrMissingAPIKey without sniffing text.
type missingKeyError struct{ label string }

func (e missingKeyError) Error() string {
	return fmt.Sprintf("%s API key is not set — add it in Settings → Transcription.", e.label)
}

func isMissingKey(err error) bool {
	var mk missingKeyError
	return errors.As(err, &mk)
}

// cloudStream builds a registered engine's streaming client.
func cloudStream(name string, cfg *config.Config) (stt.StreamEngine, error) {
	e, ok := findCloudEngine(name)
	if !ok {
		return nil, fmt.Errorf("unknown stt engine %q", name)
	}
	if e.key(cfg) == "" {
		return nil, missingKeyError{e.label}
	}
	return e.stream(cfg), nil
}
