package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/spacingmind/catchm/internal/export"
	"github.com/spacingmind/catchm/internal/summarize"
)

// ExportFile is a rendered meeting document ready to send.
type ExportFile struct {
	Body          []byte
	ContentType   string
	Filename      string
	ASCIIFilename string
}

// ErrExportFormat is returned by Export for an unsupported format.
var ErrExportFormat = errors.New(`format must be "txt", "md" or "docx"`)

// Export renders a meeting as text, Markdown or .docx in the given UI
// language ("en" or "vi").
func (s *Server) Export(id, format, lang string) (ExportFile, error) {
	var f ExportFile
	var ext string
	switch format {
	case "txt":
		f.ContentType = "text/plain; charset=utf-8"
		ext = "txt"
	case "md":
		f.ContentType = "text/markdown; charset=utf-8"
		ext = "md"
	case "docx":
		f.ContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		ext = "docx"
	default:
		return f, ErrExportFormat
	}

	m, err := s.surface.GetMeeting(id)
	if err != nil {
		return f, err
	}
	us, err := s.surface.ReadBatchTranscript(id)
	if err != nil {
		return f, err
	}
	if len(us) == 0 {
		us, _, err = s.surface.ReadLiveTranscript(id, -1)
		if err != nil {
			return f, err
		}
	}
	// The analysis only maps "Speaker 1" to a name: an unreadable one
	// costs the names, never the export.
	var people []summarize.Person
	if raw, err := s.surface.Store().ReadAnalysis(id); err == nil {
		var a summarize.Analysis
		if json.Unmarshal(raw, &a) == nil {
			people = a.People
		}
	}

	d := export.Build(m, s.surface.Namer(m), people, us, export.Lang(lang))
	switch format {
	case "txt":
		f.Body = []byte(d.Text())
	case "md":
		f.Body = []byte(d.Markdown())
	case "docx":
		body, err := d.Docx()
		if err != nil {
			return f, err
		}
		f.Body = body
	}
	f.Filename, f.ASCIIFilename = export.Filename(m, ext)
	return f, nil
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	f, err := s.Export(r.PathValue("id"), r.URL.Query().Get("format"), r.URL.Query().Get("lang"))
	if err != nil {
		if errors.Is(err, ErrExportFormat) {
			httpError(w, http.StatusBadRequest, `format must be "txt", "md" or "docx"`)
			return
		}
		respondErr(w, err)
		return
	}
	w.Header().Set("Content-Type", f.ContentType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+f.ASCIIFilename+"\"; filename*=UTF-8''"+url.PathEscape(f.Filename))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(f.Body)
}
