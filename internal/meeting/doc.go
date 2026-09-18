package meeting

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const docHeader = "<!-- catchm meeting document; frontmatter is the query surface -->"

// WriteDoc writes the meeting's markdown document with YAML frontmatter.
func (s *Store) WriteDoc(m *Meeting) error {
	raw, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(string(raw))
	b.WriteString("---\n\n")
	b.WriteString(docHeader)
	b.WriteString("\n\n# ")
	b.WriteString(m.Title)
	b.WriteString("\n")
	if m.Summary != "" {
		b.WriteString("\n" + m.Summary + "\n")
	}
	tmp := s.docPath(m.ID) + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.docPath(m.ID))
}

// parseDoc parses a meeting.md document back into a Meeting.
func parseDoc(raw string) (*Meeting, error) {
	if !strings.HasPrefix(raw, "---\n") {
		return nil, fmt.Errorf("missing frontmatter")
	}
	rest := raw[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, fmt.Errorf("unterminated frontmatter")
	}
	var m Meeting
	if err := yaml.Unmarshal([]byte(rest[:end]), &m); err != nil {
		return nil, err
	}
	body := strings.TrimLeft(rest[end+len("\n---"):], "\n")
	body = strings.TrimPrefix(body, docHeader)
	body = strings.TrimLeft(body, "\n")
	body = strings.TrimPrefix(body, "# "+m.Title)
	m.Summary = trimSpace(strings.TrimLeft(body, "\n"))
	return &m, nil
}
