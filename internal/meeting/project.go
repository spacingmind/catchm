package meeting

import "strings"

// ProjectTagPrefix marks the tag that holds a meeting's project. A
// project is stored as a plain tag (not its own frontmatter field) so
// older clients, which drop unknown fields when they re-save a meeting,
// keep it. See docs/plans/active/projects.md.
const ProjectTagPrefix = "project:"

// NormalizeProject trims a project name and collapses inner runs of
// whitespace to one space. Case is kept: it is the display name.
func NormalizeProject(name string) string {
	return strings.Join(strings.Fields(name), " ")
}

// ProjectKey is the matching key for a project name: normalized and
// lower-cased, so "Acme  Cloud" and "acme cloud" are the same project.
func ProjectKey(name string) string {
	return strings.ToLower(NormalizeProject(name))
}

// projectFromTag returns the normalized project name when tag is a
// project tag (prefix matched case-insensitively).
func projectFromTag(tag string) (string, bool) {
	t := strings.TrimSpace(tag)
	if len(t) < len(ProjectTagPrefix) || !strings.EqualFold(t[:len(ProjectTagPrefix)], ProjectTagPrefix) {
		return "", false
	}
	return NormalizeProject(t[len(ProjectTagPrefix):]), true
}

// ProjectOf returns the display name of the meeting's project, or ""
// when it has none. With several project tags the first one wins.
func ProjectOf(tags []string) string {
	for _, t := range tags {
		if name, ok := projectFromTag(t); ok && name != "" {
			return name
		}
	}
	return ""
}

// WithProject returns tags with every project tag removed and, when
// project is non-empty, a single "project:<name>" tag put first. The
// other tags keep their order. tags itself is not modified.
func WithProject(tags []string, project string) []string {
	name := NormalizeProject(project)
	out := make([]string, 0, len(tags)+1)
	if name != "" {
		out = append(out, ProjectTagPrefix+name)
	}
	for _, t := range tags {
		if _, ok := projectFromTag(t); ok {
			continue
		}
		out = append(out, t)
	}
	return out
}
