package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spacingmind/catchm/internal/batch"
	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/stt"
	"github.com/spacingmind/catchm/internal/summarize"
	"github.com/spacingmind/catchm/internal/tools"
	"github.com/spacingmind/catchm/internal/translate"
)

// sonioxCleanupTimeout bounds the startup CleanupStale pass; failures
// are ignored either way.
const sonioxCleanupTimeout = 2 * time.Minute

// refineEngineWhisperReachable is how selectRefineEngine probes
// Whisper; a var so tests can avoid real network waits.
var refineEngineWhisperReachable = stt.WhisperReachable

// newBatchEngine builds the BatchEngine refine/import uses, by engine
// name ("soniox" | "whisper"; "none" never reaches it — the caller
// passes a nil engine to batch.Refine instead). baseURL "" means
// Soniox's REST default.
func newBatchEngine(cfg *config.Config, engine string) (stt.BatchEngine, error) {
	switch engine {
	case "soniox":
		if cfg.Soniox.APIKey == "" {
			return nil, errors.New("No Soniox API key is set — add one in Settings → Transcription.")
		}
		return stt.NewSonioxAsyncEngine(cfg.Soniox.APIKey, ""), nil
	case "whisper":
		return stt.NewWhisperEngine(cfg.WhisperURL), nil
	default:
		return nil, fmt.Errorf("unknown batch engine %q", engine)
	}
}

// errEngineNotUsable marks an explicit engine choice that can't run;
// selectRefineEngine's caller answers it as {error, kind:
// "summary_not_ready"} with a 503.
type errEngineNotUsable struct{ msg string }

func (e errEngineNotUsable) Error() string { return e.msg }

// selectRefineEngine resolves the request's engine value ("", "auto"
// or an explicit name) per the plan's auto rules:
//  1. soniox meeting + Soniox key → soniox
//  2. Whisper reachable → whisper
//  3. Soniox key set → soniox
//  4. otherwise → none
//
// An explicit engine is returned as-is, failing fast when unusable.
func selectRefineEngine(ctx context.Context, cfg *config.Config, m *meeting.Meeting, engine string) (string, error) {
	if engine == "" || engine == "auto" {
		if m.Engine == "soniox" && cfg.Soniox.APIKey != "" {
			return "soniox", nil
		}
		if err := refineEngineWhisperReachable(ctx, cfg.WhisperURL); err == nil {
			return "whisper", nil
		}
		if cfg.Soniox.APIKey != "" {
			return "soniox", nil
		}
		return "none", nil
	}
	switch engine {
	case "soniox":
		if cfg.Soniox.APIKey == "" {
			return "", errEngineNotUsable{"No Soniox API key is set — add one in Settings → Transcription."}
		}
		return "soniox", nil
	case "whisper":
		if err := refineEngineWhisperReachable(ctx, cfg.WhisperURL); err != nil {
			return "", errEngineNotUsable{fmt.Sprintf("The Whisper server at %s isn't responding. Start it, or try the Soniox engine.", cfg.WhisperURL)}
		}
		return "whisper", nil
	case "none":
		return "none", nil
	default:
		return "", errors.New(`engine must be "auto", "soniox", "whisper" or "none"`)
	}
}

// engineForMeeting resolves the engine and builds the BatchEngine (nil
// for "none"). It answers engine-not-usable as a {error,
// kind:"summary_not_ready"} 503.
func (s *Server) engineForMeeting(w http.ResponseWriter, r *http.Request, m *meeting.Meeting, engine string) (string, stt.BatchEngine, bool) {
	s.mu.Lock()
	cfg := *s.cfg
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), sttStatusTimeout)
	defer cancel()
	name, err := selectRefineEngine(ctx, &cfg, m, engine)
	cancel()
	if err != nil {
		var unusable errEngineNotUsable
		if errors.As(err, &unusable) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": unusable.msg, "kind": string(summarize.KindNotReady)})
			return "", nil, false
		}
		httpError(w, http.StatusBadRequest, err.Error())
		return "", nil, false
	}
	if name == "none" {
		return name, nil, true
	}
	eng, err := s.newBatchEngine(&cfg, name)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": err.Error(), "kind": string(summarize.KindNotReady)})
		return "", nil, false
	}
	return name, eng, true
}

func (s *Server) handleRefine(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Engine string `json:"engine"`
		// Summarize "if_ready" (Improve with Soniox) re-transcribes even
		// when summaries aren't set up, skipping the summary with a
		// reason instead of failing; "" / "always" keeps refine strict.
		Summarize string `json:"summarize"`
	}
	if r.Body != nil {
		// An empty body means auto; ignore EOF from a truly empty body.
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	switch req.Engine {
	case "", "auto", "soniox", "whisper", "none":
	default:
		httpError(w, http.StatusBadRequest, `engine must be "auto", "soniox", "whisper" or "none"`)
		return
	}
	if req.Summarize != "" && req.Summarize != "always" && req.Summarize != "if_ready" {
		httpError(w, http.StatusBadRequest, `summarize must be "always" or "if_ready"`)
		return
	}
	ifReady := req.Summarize == "if_ready"
	// skipped is the plain reason the summary didn't run in if_ready
	// mode; the transcript is still improved.
	var skipped string
	skip := func(err error) bool {
		if !ifReady {
			return false
		}
		var se *summarize.Error
		if errors.As(err, &se) && se.Message != "" {
			skipped = se.Message
		} else {
			skipped = err.Error()
		}
		return true
	}
	m, err := s.surface.GetMeeting(id)
	if err != nil {
		respondRefineErr(w, err)
		return
	}
	// A meeting that can't be refined answers that first, before any
	// summary readiness probe.
	if err := batch.CheckRefinable(s.surface.Store(), id); err != nil {
		respondRefineErr(w, err)
		return
	}
	var translator translate.Translator
	if m.TranslateTo != "" {
		if t, err := s.newTranslator(s.cfg, s.cfg.Translate.BatchModel); err == nil {
			translator = t
		}
	}
	// Build the summarizer before the (slow) batch pass: its checks are
	// config-only, so a missing cloud consent or provider setup answers
	// at once, and nothing reaches the cloud without consent.
	s.mu.Lock()
	cfg := *s.cfg
	s.mu.Unlock()
	auth := s.cloudAuth()
	sum, err := s.newSummarizer(&cfg, auth)
	if err != nil && !skip(err) {
		respondSummaryErr(w, err)
		return
	}
	// Then the status endpoint's probes (Ollama up with the model, cloud
	// quota left), so a summary that can't run fails before minutes of
	// batch transcription. An unreachable quota endpoint doesn't block.
	if skipped == "" {
		readyCtx, cancel := context.WithTimeout(r.Context(), refineReadyTimeout)
		st := summaryReadiness(readyCtx, &cfg, auth, false)
		cancel()
		if err := st.err(); err != nil && !skip(err) {
			respondSummaryErr(w, err)
			return
		}
	}
	engineName, engine, ok := s.engineForMeeting(w, r, m, req.Engine)
	if !ok {
		return
	}
	if err := batch.Refine(r.Context(), s.surface.Store(), id, engine, translator); err != nil {
		respondRefineErr(w, err)
		return
	}
	if engineName != "none" {
		if err := s.surface.Store().UpdateDoc(id, func(mm *meeting.Meeting) error {
			mm.RefinedWith = engineName
			return nil
		}); err != nil {
			respondRefineErr(w, err)
			return
		}
	}
	if skipped == "" {
		if err := summarize.SummarizeMeeting(r.Context(), s.surface.Store(), id, sum); err != nil && !skip(err) {
			respondSummaryErr(w, err)
			return
		}
	}
	m, err = s.surface.GetMeeting(id)
	if err != nil || skipped == "" {
		respond(w, m, err)
		return
	}
	// The meeting JSON plus why the summary wasn't rewritten.
	writeJSON(w, http.StatusOK, struct {
		*meeting.Meeting
		SummarySkipped string `json:"summary_skipped"`
	}{m, skipped})
}

// importMaxBodyBytes caps POST /api/meetings/import at 1 GiB. A var so
// the size-limit test can shrink it instead of sending a gigabyte.
var importMaxBodyBytes int64 = 1 << 30

// importAllowedExts is the plan's extension allowlist.
var importAllowedExts = map[string]bool{
	"wav": true, "mp3": true, "m4a": true, "mp4": true,
	"ogg": true, "webm": true, "flac": true, "aac": true,
}

// handleImportMeeting turns an uploaded audio file into an ended,
// imported meeting and re-transcribes it with the selected engine.
// Multipart fields: file (required), plus optional title, lang,
// translate_to, two_way, engine, project. The upload streams straight to disk.
func (s *Server) handleImportMeeting(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, importMaxBodyBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		httpError(w, http.StatusBadRequest, "expected a multipart upload")
		return
	}

	var fileExt, title, lang, translateTo, engineName, importFileName, project string
	var twoWay bool
	var tmpPath, dstDir string
	defer func() {
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
	}()

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			respondImportMaxBytes(w, err)
			return
		}
		name := part.FormName()
		switch name {
		case "file":
			if tmpPath != "" {
				rejectImport(w, r, http.StatusBadRequest, map[string]string{"error": "send exactly one file"})
				return
			}
			ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(part.FileName())), ".")
			if !importAllowedExts[ext] {
				rejectImport(w, r, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf(
					"Unsupported file type .%s. Allowed: wav mp3 m4a mp4 ogg webm flac aac.", ext)})
				return
			}
			// Fields sent ahead of the file (the web client does) let an
			// explicit, unusable engine fail before the upload is read.
			if engineName == "whisper" && ext != "wav" {
				rejectImport(w, r, http.StatusBadRequest, map[string]string{
					"error": "The Whisper engine only accepts WAV files. Use the Soniox engine for other formats."})
				return
			}
			if engineName == "soniox" {
				s.mu.Lock()
				noKey := s.cfg.Soniox.APIKey == ""
				s.mu.Unlock()
				if noKey {
					rejectImport(w, r, http.StatusServiceUnavailable, map[string]string{
						"error": "No Soniox API key is set — add one in Settings → Transcription.",
						"kind":  string(summarize.KindNotReady)})
					return
				}
			}
			fileExt = ext
			importFileName = strings.TrimSuffix(part.FileName(), filepath.Ext(part.FileName()))
			dstDir = filepath.Join(s.surface.Store().Root(), ".import-tmp")
			if err := os.MkdirAll(dstDir, 0o700); err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			f, err := os.CreateTemp(dstDir, "import-*")
			if err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if _, err := io.Copy(f, part); err != nil {
				f.Close()
				respondImportMaxBytes(w, err)
				return
			}
			if err := f.Close(); err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			tmpPath = f.Name()
		default:
			value, err := io.ReadAll(io.LimitReader(part, 4096))
			if err != nil {
				httpError(w, http.StatusBadRequest, "bad multipart body")
				return
			}
			v := string(value)
			switch name {
			case "title":
				title = v
			case "lang":
				lang = v
			case "translate_to":
				translateTo = v
			case "two_way":
				twoWay = v == "true" || v == "1"
			case "engine":
				engineName = v
			case "project":
				project = v
			}
		}
	}
	if tmpPath == "" {
		httpError(w, http.StatusBadRequest, "file is required")
		return
	}
	if title == "" {
		title = importFileName
	}
	if twoWay && (lang == "" || translateTo == "") {
		httpError(w, http.StatusBadRequest, "two_way requires both lang and translate_to (language A and B)")
		return
	}
	var tags []string
	if meeting.NormalizeProject(project) != "" {
		p, err := tools.ValidProjectName(project)
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		tags = meeting.WithProject(nil, p)
	}

	// Resolve the engine before creating the meeting, so a bad request
	// never leaves a half-built meeting behind.
	s.mu.Lock()
	cfg := *s.cfg
	s.mu.Unlock()
	// Bounded like refine's probe: a port that accepts but never answers
	// must not hang the import.
	selCtx, selCancel := context.WithTimeout(r.Context(), sttStatusTimeout)
	name, err := selectRefineEngine(selCtx, &cfg, &meeting.Meeting{}, engineName)
	selCancel()
	if err != nil {
		var unusable errEngineNotUsable
		if errors.As(err, &unusable) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": unusable.msg, "kind": string(summarize.KindNotReady)})
			return
		}
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if name == "whisper" && fileExt != "wav" {
		httpError(w, http.StatusBadRequest, "The Whisper engine only accepts WAV files. Use the Soniox engine for other formats.")
		return
	}
	var engine stt.BatchEngine
	if name != "none" {
		engine, err = s.newBatchEngine(&cfg, name)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": err.Error(), "kind": string(summarize.KindNotReady)})
			return
		}
	}

	now := time.Now().UTC()
	m, err := s.surface.StartMeetingWithOptions(title, meeting.StartOptions{
		Lang:        lang,
		TranslateTo: translateTo,
		TwoWay:      twoWay,
		Tags:        tags,
	})
	if err != nil {
		respondErr(w, err)
		return
	}
	if err := s.surface.EndMeeting(m.ID); err != nil {
		respondErr(w, err)
		return
	}
	audioPath := filepath.Join(s.surface.Store().Root(), m.ID, "audio."+fileExt)
	if err := os.Rename(tmpPath, audioPath); err != nil {
		respondErr(w, err)
		return
	}
	tmpPath = ""
	if err := s.surface.Store().UpdateDoc(m.ID, func(mm *meeting.Meeting) error {
		mm.Imported = true
		mm.StartedAt = now
		if mm.EndedAt == nil {
			mm.EndedAt = &now
		}
		return nil
	}); err != nil {
		respondErr(w, err)
		return
	}

	if engine != nil {
		var translator translate.Translator
		if translateTo != "" {
			if t, terr := s.newTranslator(&cfg, cfg.Translate.BatchModel); terr == nil {
				translator = t
			}
		}
		if err := batch.Refine(r.Context(), s.surface.Store(), m.ID, engine, translator); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error":      "Transcription failed: " + err.Error(),
				"kind":       string(summarize.KindFailed),
				"meeting_id": m.ID,
			})
			return
		}
		// The upload has no wall-clock span of its own: end it at the last
		// transcribed line so the list's time range and cost estimates
		// reflect the recording's length, not a zero-length meeting.
		var end *time.Time
		if us, rerr := s.surface.Store().ReadBatchTranscript(m.ID); rerr == nil && len(us) > 0 {
			last := us[len(us)-1]
			t := last.At
			if last.End != nil {
				t = *last.End
			}
			end = &t
		}
		if err := s.surface.Store().UpdateDoc(m.ID, func(mm *meeting.Meeting) error {
			mm.RefinedWith = name
			if end != nil && end.After(mm.StartedAt) {
				mm.EndedAt = end
			}
			return nil
		}); err != nil {
			respondErr(w, err)
			return
		}
	}

	m, err = s.surface.GetMeeting(m.ID)
	respond(w, m, err)
}

// rejectImport answers an import rejected mid-upload. It drains the
// rest of the (MaxBytesReader-capped) body first: a response sent while
// the client is still uploading can be lost to a connection reset,
// leaving the UI with a bare network error instead of the reason.
func rejectImport(w http.ResponseWriter, r *http.Request, status int, body map[string]string) {
	_, _ = io.Copy(io.Discard, r.Body)
	writeJSON(w, status, body)
}

// respondImportMaxBytes maps a MaxBytesReader overflow to 413; any
// other error is a plain 400.
func respondImportMaxBytes(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		httpError(w, http.StatusRequestEntityTooLarge, "The recording is larger than the 1 GiB import limit.")
		return
	}
	httpError(w, http.StatusBadRequest, "bad multipart body: "+err.Error())
}
