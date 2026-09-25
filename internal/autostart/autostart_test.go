package autostart

import "testing"

func TestCommandLineQuotesPathWithSpaces(t *testing.T) {
	got := CommandLine(`C:\Program Files\CatchM\catchm-desktop.exe`)
	want := `"C:\Program Files\CatchM\catchm-desktop.exe" --autostart`
	if got != want {
		t.Fatalf("CommandLine() = %q, want %q", got, want)
	}
}

func TestCommandLineQuotesEvenWithoutSpaces(t *testing.T) {
	// Always quoted: consistent parsing regardless of the install path,
	// not conditional on whether THIS particular path happens to need it.
	got := CommandLine(`C:\CatchM\catchm-desktop.exe`)
	want := `"C:\CatchM\catchm-desktop.exe" --autostart`
	if got != want {
		t.Fatalf("CommandLine() = %q, want %q", got, want)
	}
}

// fakeStore is a Store the toggle logic can be tested against without
// a real Windows registry.
type fakeStore struct {
	enabled bool
	setErr  error
}

func (f *fakeStore) IsEnabled() (bool, error) { return f.enabled, nil }
func (f *fakeStore) SetEnabled(enabled bool) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.enabled = enabled
	return nil
}

func TestFakeStoreRoundTrip(t *testing.T) {
	var s Store = &fakeStore{}
	if enabled, err := s.IsEnabled(); err != nil || enabled {
		t.Fatalf("IsEnabled() = (%v, %v), want (false, nil)", enabled, err)
	}
	if err := s.SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled(true) error = %v", err)
	}
	if enabled, err := s.IsEnabled(); err != nil || !enabled {
		t.Fatalf("IsEnabled() after SetEnabled(true) = (%v, %v), want (true, nil)", enabled, err)
	}
	if err := s.SetEnabled(false); err != nil {
		t.Fatalf("SetEnabled(false) error = %v", err)
	}
	if enabled, _ := s.IsEnabled(); enabled {
		t.Fatal("IsEnabled() after SetEnabled(false) = true, want false")
	}
}
