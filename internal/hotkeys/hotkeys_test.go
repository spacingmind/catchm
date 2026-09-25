package hotkeys

import "testing"

func TestParseComboBasic(t *testing.T) {
	c, err := ParseCombo("Ctrl+Shift+L")
	if err != nil {
		t.Fatalf("ParseCombo() error = %v", err)
	}
	if c.Key != "L" || len(c.Mods) != 2 {
		t.Fatalf("ParseCombo() = %+v, want Key=L, 2 mods", c)
	}
	if got := c.String(); got != "Ctrl+Shift+L" {
		t.Fatalf("String() = %q, want %q", got, "Ctrl+Shift+L")
	}
}

func TestParseComboCaseInsensitiveAndWhitespace(t *testing.T) {
	c, err := ParseCombo(" ctrl + alt + m ")
	if err != nil {
		t.Fatalf("ParseCombo() error = %v", err)
	}
	if got := c.String(); got != "Ctrl+Alt+M" {
		t.Fatalf("String() = %q, want %q", got, "Ctrl+Alt+M")
	}
}

// Modifier order in the input must not matter — the canonical String()
// output is always Ctrl, Alt, Shift, Win.
func TestParseComboNormalizesModifierOrder(t *testing.T) {
	c, err := ParseCombo("Shift+Ctrl+F5")
	if err != nil {
		t.Fatalf("ParseCombo() error = %v", err)
	}
	if got := c.String(); got != "Ctrl+Shift+F5" {
		t.Fatalf("String() = %q, want %q", got, "Ctrl+Shift+F5")
	}
}

func TestParseComboSynonyms(t *testing.T) {
	cases := map[string]string{
		"Control+Enter": "Ctrl+Return",
		"Win+Esc":       "Win+Escape",
		"Super+A":       "Win+A",
		"Cmd+Tab":       "Win+Tab",
	}
	for in, want := range cases {
		c, err := ParseCombo(in)
		if err != nil {
			t.Fatalf("ParseCombo(%q) error = %v", in, err)
		}
		if got := c.String(); got != want {
			t.Fatalf("ParseCombo(%q).String() = %q, want %q", in, got, want)
		}
	}
}

func TestParseComboRejectsNoModifier(t *testing.T) {
	if _, err := ParseCombo("L"); err == nil {
		t.Fatal("ParseCombo(\"L\") succeeded, want an error (no modifier)")
	}
}

func TestParseComboRejectsNoKey(t *testing.T) {
	if _, err := ParseCombo("Ctrl+Shift"); err == nil {
		t.Fatal("ParseCombo(\"Ctrl+Shift\") succeeded, want an error (no key)")
	}
}

func TestParseComboRejectsTwoKeys(t *testing.T) {
	if _, err := ParseCombo("Ctrl+A+B"); err == nil {
		t.Fatal("ParseCombo(\"Ctrl+A+B\") succeeded, want an error (two keys)")
	}
}

func TestParseComboRejectsUnrecognizedKey(t *testing.T) {
	if _, err := ParseCombo("Ctrl+PrintScreen"); err == nil {
		t.Fatal("ParseCombo(\"Ctrl+PrintScreen\") succeeded, want an error (not in the supported vocabulary)")
	}
}

func TestParseComboRejectsEmptyAndMalformed(t *testing.T) {
	for _, s := range []string{"", "   ", "Ctrl++L", "Ctrl+Ctrl+L"} {
		if _, err := ParseCombo(s); err == nil {
			t.Fatalf("ParseCombo(%q) succeeded, want an error", s)
		}
	}
}

func TestDefaultBindingsAreValid(t *testing.T) {
	b := DefaultBindings()
	if len(b) != len(Actions) {
		t.Fatalf("DefaultBindings() has %d entries, want %d", len(b), len(Actions))
	}
	if errs := ValidateBindings(b); len(errs) != 0 {
		t.Fatalf("ValidateBindings(DefaultBindings()) = %v, want no errors", errs)
	}
}

// The exact scenario DefaultBindings' own comment explains: M21's
// focused-window compact shortcut is Ctrl+Shift+L, so the global
// default must not collide with it.
func TestDefaultBindingsDontCollideWithM21FocusedShortcut(t *testing.T) {
	b := DefaultBindings()
	if b[ActionToggleCompact] == "Ctrl+Shift+L" {
		t.Fatal("ToggleCompact default collides with M21's focused-window Ctrl+Shift+L shortcut")
	}
}

func TestValidateBindingsCatchesDuplicateCombo(t *testing.T) {
	b := Bindings{
		ActionShowHide:         "Ctrl+Alt+C",
		ActionStartStopMeeting: "Ctrl+Alt+C", // same as above, different action
		ActionToggleCompact:    "Ctrl+Alt+L",
	}
	errs := ValidateBindings(b)
	if errs[ActionShowHide] == "" || errs[ActionStartStopMeeting] == "" {
		t.Fatalf("ValidateBindings() = %v, want both colliding actions flagged", errs)
	}
	if errs[ActionToggleCompact] != "" {
		t.Fatalf("ValidateBindings() flagged %s, want it untouched", ActionToggleCompact)
	}
}

// Two combos that are the SAME after normalization (different input
// order/case) must still be caught as a collision.
func TestValidateBindingsCatchesCollisionAfterNormalization(t *testing.T) {
	b := Bindings{
		ActionShowHide:         "ctrl+alt+c",
		ActionStartStopMeeting: "Alt+Ctrl+C",
		ActionToggleCompact:    "Ctrl+Alt+L",
	}
	errs := ValidateBindings(b)
	if errs[ActionShowHide] == "" || errs[ActionStartStopMeeting] == "" {
		t.Fatalf("ValidateBindings() = %v, want both flagged (same combo, different case/order)", errs)
	}
}

func TestValidateBindingsCatchesUnparseable(t *testing.T) {
	b := DefaultBindings()
	b[ActionShowHide] = "not a combo"
	errs := ValidateBindings(b)
	if errs[ActionShowHide] == "" {
		t.Fatalf("ValidateBindings() = %v, want %s flagged", errs, ActionShowHide)
	}
	if errs[ActionStartStopMeeting] != "" || errs[ActionToggleCompact] != "" {
		t.Fatalf("ValidateBindings() = %v, want only %s flagged", errs, ActionShowHide)
	}
}

func TestValidateBindingsCatchesMissingAction(t *testing.T) {
	b := Bindings{ActionShowHide: "Ctrl+Alt+C"} // missing the other two
	errs := ValidateBindings(b)
	if errs[ActionStartStopMeeting] == "" || errs[ActionToggleCompact] == "" {
		t.Fatalf("ValidateBindings() = %v, want the missing actions flagged", errs)
	}
}

func TestBindingsCloneIsIndependent(t *testing.T) {
	orig := DefaultBindings()
	clone := orig.Clone()
	clone[ActionShowHide] = "Ctrl+Alt+X"
	if orig[ActionShowHide] == "Ctrl+Alt+X" {
		t.Fatal("Clone() shares storage with the original")
	}
}
