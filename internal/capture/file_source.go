package capture

import (
	"sync"

	"github.com/spacingmind/catchm/internal/audio"
	"github.com/spacingmind/catchm/internal/stt"
)

// DefaultChunkBytes is a reasonable chunk size for feeding a FileSource:
// 100ms of 16kHz mono 16-bit PCM.
const DefaultChunkBytes = audio.SampleRate * audio.Channels * (audio.BitsPerSample / 8) / 10

// FileSource replays a WAV file as PCM chunks, standing in for a
// microphone in tests and offline runs.
type FileSource struct {
	ch       chan stt.PCM
	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{}
}

// NewFileSource reads path (a 16kHz mono 16-bit PCM WAV file) and starts
// replaying it in chunkBytes-sized chunks; chunkBytes <= 0 uses
// DefaultChunkBytes.
func NewFileSource(path string, chunkBytes int) (*FileSource, error) {
	pcm, err := audio.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if chunkBytes <= 0 {
		chunkBytes = DefaultChunkBytes
	}

	f := &FileSource{
		ch:   make(chan stt.PCM),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go func() {
		defer close(f.ch)
		defer close(f.done)
		for i := 0; i < len(pcm); i += chunkBytes {
			end := i + chunkBytes
			if end > len(pcm) {
				end = len(pcm)
			}
			select {
			case f.ch <- stt.PCM(pcm[i:end]):
			case <-f.stop:
				return
			}
		}
	}()
	return f, nil
}

func (f *FileSource) Chunks() <-chan stt.PCM { return f.ch }

// Close stops replay (if not already finished) and waits for the feeding
// goroutine to exit.
func (f *FileSource) Close() error {
	f.stopOnce.Do(func() { close(f.stop) })
	<-f.done
	return nil
}
