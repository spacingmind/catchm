//go:build windows

package main

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"
)

// The M23 babysitter ran the installer with bare "/S", so NSIS
// installed to its own default dir (which changed when the company
// name changed) instead of over the running copy — the babysitter
// then relaunched the stale old exe. /D pins the install dir to the
// running exe's directory; per NSIS rules it must be the LAST
// argument and UNQUOTED even with spaces (NSIS re-joins the command
// line tail itself — a quoted value would arrive with literal quote
// characters in the path).

func TestInstallerArgsSlashDIsLastAndUnquoted(t *testing.T) {
	cases := []struct {
		name string
		dir  string
		want string
	}{
		{"program files with spaces", `C:\Program Files\SpacingMind\CatchM`, `/D=C:\Program Files\SpacingMind\CatchM`},
		{"plain dir", `C:\Apps\CatchM`, `/D=C:\Apps\CatchM`},
		// Clean keeps the separator on a drive root (an exe can live
		// there) — "/D=C:\" is the documented NSIS drive-root form.
		{"drive root", `C:\`, `/D=C:\`},
		{"trailing backslash is dropped", `C:\Program Files\CatchM\`, `/D=C:\Program Files\CatchM`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := installerArgs(tc.dir)
			if len(args) != 2 {
				t.Fatalf("installerArgs(%q) = %v, want exactly [/S, /D=...] — extra args after /D would be swallowed into the path", tc.dir, args)
			}
			if args[0] != "/S" {
				t.Errorf("installerArgs(%q)[0] = %q, want /S (silent install — no UI after the app quits)", tc.dir, args[0])
			}
			if args[1] != tc.want {
				t.Errorf("installerArgs(%q)[1] = %q, want %q", tc.dir, args[1], tc.want)
			}
			if strings.Contains(args[1], `"`) {
				t.Errorf("installerArgs(%q) = %q, /D must stay unquoted per NSIS rules", tc.dir, args[1])
			}
		})
	}
}

// The babysitter's Start-Process -ArgumentList joins the args with
// "', '" — quoting each element but NOT /D's value in a way NSIS
// could misread. The net effect on the installer's command line is
// '/S' followed by '/D=<dir with spaces unquoted, last>'.
func TestBabysitterScriptRunsInstallerWithSlashD(t *testing.T) {
	script := babysitterScript(1234, `C:\Users\a\AppData\Local\Temp\setup.exe`, `C:\Program Files\SpacingMind\CatchM\CatchM.exe`)

	want := "-ArgumentList '/S', '/D=C:\\Program Files\\SpacingMind\\CatchM'"
	if !strings.Contains(script, want) {
		t.Errorf("babysitter script missing %q in the Start-Process call:\n%s", want, script)
	}
	// /D must be the last argument in the joined list too.
	if strings.Contains(script, "/D=") && strings.Index(script, "CatchM'") < strings.Index(script, "/D=") {
		t.Errorf("babysitter script has arguments after /D:\n%s", script)
	}
	if !strings.Contains(script, "Start-Process explorer.exe $exe") {
		t.Error("babysitter script lost the unelevated relaunch via explorer.exe")
	}
	if !strings.Contains(script, "$oldPid = 1234") || !strings.Contains(script, "Wait-Process -Id $oldPid") {
		t.Error("babysitter script lost the wait for the old pid (the exe lock)")
	}
}

// Paths travel base64-encoded in the script body, so an installer
// path containing quotes/backslashes can never break out of the
// PowerShell string literal.
func TestBabysitterScriptEmbedsPathsBase64(t *testing.T) {
	installer := `C:\Temp\odd "name"\setup.exe`
	script := babysitterScript(1, installer, `C:\Apps\CatchM.exe`)
	if !strings.Contains(script, base64.StdEncoding.EncodeToString([]byte(installer))) {
		t.Error("installer path not embedded as base64 in the script body")
	}
	// A single quote in a decoded path is harmless (it's decoded at
	// runtime), and a quote in the RAW script body would be a
	// breaking-out hazard outside the base64 blocks.
	if strings.Count(script, base64.StdEncoding.EncodeToString([]byte(installer))) != 1 {
		t.Error("installer path should appear exactly once (base64 block only)")
	}
}

// encodePowerShell must produce UTF-16LE + base64 — the encoding
// powershell -EncodedCommand is specified to accept. Round-trip it.
func TestEncodePowerShellRoundTrip(t *testing.T) {
	script := babysitterScript(42, `C:\setup.exe`, `C:\Apps\CatchM.exe`)
	enc := encodePowerShell(script)
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatalf("EncodedCommand payload isn't valid base64: %v", err)
	}
	if len(raw)%2 != 0 {
		t.Fatalf("EncodedCommand payload isn't UTF-16 (odd byte count %d)", len(raw))
	}
	u16 := make([]uint16, len(raw)/2)
	for i := range u16 {
		u16[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8 // little-endian
	}
	if got := string(utf16.Decode(u16)); got != script {
		t.Errorf("encodePowerShell round-trip mismatch:\n got %q\nwant %q", got, script)
	}
}
