package stt

// EngineState is a connection-state label for the live view's status
// row (M21 §1). It is a plain string, not a closed enum, because a
// future engine may have connection states neither current engine
// produces (see EngineStatusReporter's doc comment for what "connecting"
// and "reconnecting" would mean and why neither Soniox nor Whisper
// currently reports them).
type EngineState string

const (
	// EngineStreaming means the engine is actively able to process
	// audio: for Soniox, its websocket session is open; for Whisper,
	// its per-chunk request loop is running (Whisper has no persistent
	// connection to "stream" over, but the loop being alive and willing
	// to try the next chunk is the analogous truthful signal).
	EngineStreaming EngineState = "streaming"
	// EngineError means the engine hit a failure it cannot recover
	// from within this session: for Soniox, its websocket connection
	// dropped unexpectedly (not as part of an intentional Stop). No
	// current engine produces "connecting" (both engines' Stream
	// establishes or fails their connection synchronously, before a
	// Pipeline — and so a /levels entry — exists to observe an
	// in-between state) or "reconnecting" (neither engine retries a
	// dropped connection; Soniox's session simply ends).
	EngineError EngineState = "error"
)

// EngineStatusReporter is implemented by StreamEngines that track a
// connection state worth surfacing through GET /api/meetings/{id}/levels
// (M21 §2). Optional: a StreamEngine with nothing truthful to report
// (none currently, but a future one might) simply doesn't implement it,
// the same way capture.DropCounter is an optional capability sources
// opt into. Must be cheap and lock-light — no disk I/O, callable from
// an HTTP handler polled at ~5 Hz.
type EngineStatusReporter interface {
	// EngineStatus returns the current state and, for EngineError, a
	// human-readable reason. errMsg is empty for every other state.
	EngineStatus() (state EngineState, errMsg string)
}
