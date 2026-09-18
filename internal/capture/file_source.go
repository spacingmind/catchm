package capture

import (
	"sync"
	"time"

	"github.com/spacingmind/catchm/internal/audio"
	"github.com/spacingmind/catchm/internal/stt"
)

// bytesPerSecond is how many PCM bytes one second of audio.SampleRate/
// Channels/BitsPerSample-shaped audio takes up — used to pace replay to
// real time (see NewFileSource's doc comment for why this matters).
const bytesPerSecond = audio.SampleRate * audio.Channels * (audio.BitsPerSample / 8)

// DefaultChunkBytes is a reasonable chunk size for feeding a FileSource:
// 100ms of 16kHz mono 16-bit PCM.
const DefaultChunkBytes = audio.SampleRate * audio.Channels * (audio.BitsPerSample / 8) / 10

// FileSource replays a WAV file as PCM chunks, standing in for a
// microphone in tests and offline runs. Chunks are paced to the audio's
// real-time duration (not delivered as fast as the reader can consume
// them): a cloud realtime STT engine like Soniox needs roughly
// real-time delivery to have any chance to process and return results
// before the stream ends — dumping the whole file in a burst gives it
// no wall-clock time to respond before Stop() tears the session down.
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
		ticker := time.NewTicker(chunkDuration(chunkBytes))
		defer ticker.Stop()
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
			if end == len(pcm) {
				break
			}
			select {
			case <-ticker.C:
			case <-f.stop:
				return
			}
		}
	}()
	return f, nil
}

func (f *FileSource) Chunks() <-chan stt.PCM { return f.ch }

// chunkDuration is how long chunkBytes of PCM represents in real time.
func chunkDuration(chunkBytes int) time.Duration {
	return time.Duration(chunkBytes) * time.Second / bytesPerSecond
}

// Close stops replay (if not already finished) and waits for the feeding
// goroutine to exit.
func (f *FileSource) Close() error {
	f.stopOnce.Do(func() { close(f.stop) })
	<-f.done
	return nil
}
