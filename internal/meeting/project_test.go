package meeting

import (
	"reflect"
	"testing"
)

func TestProjectOf(t *testing.T) {
	cases := []struct {
		tags []string
		want string
	}{
		{nil, ""},
		{[]string{"acme", "q3"}, ""},
		{[]string{"acme", "project:CatchM Cloud"}, "CatchM Cloud"},
		{[]string{"Project:  CatchM   Cloud "}, "CatchM Cloud"},
		{[]string{"PROJECT:x"}, "x"},
		{[]string{"project:", "project:Real"}, "Real"},
		{[]string{"project:First", "project:Second"}, "First"},
		{[]string{"projects:nope"}, ""},
		{[]string{"proj"}, ""},
	}
	for _, c := range cases {
		if got := ProjectOf(c.tags); got != c.want {
			t.Errorf("ProjectOf(%q) = %q, want %q", c.tags, got, c.want)
		}
	}
}

func TestWithProject(t *testing.T) {
	cases := []struct {
		tags    []string
		project string
		want    []string
	}{
		{nil, "Acme", []string{"project:Acme"}},
		{nil, "", []string{}},
		{[]string{"a", "b"}, " Acme   Cloud ", []string{"project:Acme Cloud", "a", "b"}},
		{[]string{"a", "project:Old", "b"}, "New", []string{"project:New", "a", "b"}},
		{[]string{"a", "Project:Old", "b", "project:Other"}, "", []string{"a", "b"}},
		{[]string{"project:Old"}, "   ", []string{}},
	}
	for _, c := range cases {
		in := append([]string(nil), c.tags...)
		got := WithProject(c.tags, c.project)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("WithProject(%q, %q) = %q, want %q", c.tags, c.project, got, c.want)
		}
		if !reflect.DeepEqual(in, c.tags) {
			t.Errorf("WithProject modified its input: %q", c.tags)
		}
	}
}

func TestProjectKey(t *testing.T) {
	if ProjectKey(" Acme  Cloud") != ProjectKey("acme cloud") {
		t.Fatal("keys should match case- and space-insensitively")
	}
	if ProjectKey("Acme") == ProjectKey("Acme Cloud") {
		t.Fatal("different projects share a key")
	}
}
