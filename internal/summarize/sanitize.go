package summarize

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// sanitizeReply repairs a model reply's JSON in place before it is
// decoded into an Analysis (the sanitize step of Understand-Anything's
// schema.ts): alias field names are renamed, null fields dropped, a bare
// string item becomes an object, and scalars are coerced to the types
// the Analysis fields expect. It returns the fixes it made.
func sanitizeReply(obj map[string]any) []string {
	s := &sanitizer{}
	s.object("", obj, topLevelAliases)
	for _, field := range slices.Sorted(maps.Keys(itemMainField)) {
		main := itemMainField[field]
		if items, ok := obj[field]; ok {
			obj[field] = s.items(field, items, main)
		}
	}
	if s.coerced > 0 {
		s.issues = append(s.issues, fmt.Sprintf("auto-corrected: %d value(s) coerced to their expected types", s.coerced))
	}
	return s.issues
}

// topLevelAliases renames reply keys a model may use for Analysis's.
var topLevelAliases = map[string]string{
	"action_items": "actions", "actionitems": "actions", "tasks": "actions", "todos": "actions",
	"participants": "people", "attendees": "people", "speakers": "people", "persons": "people",
	"themes": "topics",
}

// itemMainField is, per item collection, the field a bare string item
// stands for.
var itemMainField = map[string]string{
	"topics": "title", "decisions": "text", "actions": "text",
	"people": "name", "entities": "name", "flows": "name",
}

// itemAliases renames item fields a model may use for the schema's.
var itemAliases = map[string]map[string]string{
	"topics": {"name": "title", "topic": "title", "slug": "key"},
	"decisions": {"decision": "text", "description": "text", "title": "text",
		"topic_id": "topic", "context": "scope"},
	"actions": {"task": "text", "action": "text", "description": "text", "title": "text",
		"assignee": "owner", "owner_key": "owner", "deadline": "due", "due_date": "due",
		"deadline_text": "due_text", "topic_id": "topic"},
	"people":   {"display_name": "name", "alias": "aliases", "speaker_label": "speaker"},
	"entities": {"title": "name", "type": "kind", "category": "kind"},
	"flows":    {"title": "name"},
	"steps":    {"description": "text", "step": "text", "title": "text"},
}

// Field types other than string, by field name (at any depth).
var (
	seqsFields    = map[string]bool{"evidence": true}
	intFields     = map[string]bool{"order": true, "version": true}
	stringsFields = map[string]bool{"aliases": true, "issues": true}
	// nested item collections: field → main field for bare strings.
	nestedItems = map[string]string{"steps": "text", "next": "to"}
)

type sanitizer struct {
	issues  []string
	coerced int
}

var digits = regexp.MustCompile(`\d+`)

// object renames aliased keys (when the canonical key is absent), drops
// null values and coerces each field's value.
func (s *sanitizer) object(where string, obj map[string]any, aliases map[string]string) {
	for _, from := range slices.Sorted(maps.Keys(aliases)) {
		to := aliases[from]
		v, ok := obj[from]
		if !ok {
			continue
		}
		delete(obj, from)
		if _, has := obj[to]; has {
			continue
		}
		obj[to] = v
		s.issues = append(s.issues, fmt.Sprintf("auto-corrected: %sfield %q renamed %q", where, from, to))
	}
	for k, v := range obj {
		if v == nil {
			delete(obj, k)
			continue
		}
		if _, isItems := itemMainField[k]; isItems && where == "" {
			continue // top-level collections are handled by items
		}
		obj[k] = s.value(k, v)
	}
}

// items coerces a collection to a list of objects.
func (s *sanitizer) items(field string, v any, main string) any {
	list, ok := v.([]any)
	if !ok {
		s.coerced++
		list = []any{v} // a lone item
	}
	out := make([]any, 0, len(list))
	for i, it := range list {
		switch x := it.(type) {
		case map[string]any:
			s.object(fmt.Sprintf("%s[%d] ", field, i), x, itemAliases[field])
			out = append(out, x)
		case nil:
			s.coerced++
		default:
			s.coerced++
			out = append(out, map[string]any{main: scalarString(x)})
		}
	}
	return out
}

// value coerces one field's value to its expected type.
func (s *sanitizer) value(field string, v any) any {
	switch {
	case seqsFields[field]:
		return s.seqs(v)
	case intFields[field]:
		if f, ok := v.(float64); ok {
			return float64(int64(f))
		}
		s.coerced++
		n, _ := strconv.Atoi(strings.TrimSpace(scalarString(v)))
		return n
	case stringsFields[field]:
		list, ok := v.([]any)
		if !ok {
			s.coerced++
			list = []any{v}
		}
		out := make([]any, 0, len(list))
		for _, it := range list {
			if _, isStr := it.(string); !isStr {
				s.coerced++
			}
			if it != nil {
				out = append(out, scalarString(it))
			}
		}
		return out
	case nestedItems[field] != "":
		return s.items(field, v, nestedItems[field])
	}
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		s.object(field+".", x, nil)
		return x
	default:
		s.coerced++
		return scalarString(x)
	}
}

// seqs coerces evidence to a list of integers: a lone number, numeric
// strings ("12", "[12]", "12, 14") and floats are all accepted.
func (s *sanitizer) seqs(v any) any {
	list, ok := v.([]any)
	if !ok {
		s.coerced++
		list = []any{v}
	}
	out := make([]any, 0, len(list))
	for _, it := range list {
		switch x := it.(type) {
		case float64:
			if x != float64(int64(x)) {
				s.coerced++
			}
			out = append(out, float64(int64(x)))
		case string:
			s.coerced++
			for _, d := range digits.FindAllString(x, -1) {
				if n, err := strconv.ParseInt(d, 10, 64); err == nil {
					out = append(out, float64(n))
				}
			}
		default:
			s.coerced++
		}
	}
	return out
}

// scalarString renders a JSON scalar as a string.
func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}
