package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func publishedAtOf(t *testing.T) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, "2026-09-20T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

// releaseFixture serves a release whose installer bytes are installer
// and whose checksum asset is derived from those bytes (or corrupted,
// per corruptSum).
type releaseFixture struct {
	installer []byte
	corrupt   bool // publish a checksum that does NOT match
	dropSum   bool // publish no checksum asset at all
	dropBin   bool // publish no installer asset
}

func serveRelease(t *testing.T, f releaseFixture) *httptest.Server {
	t.Helper()
	sum := sha256.Sum256(f.installer)
	sumLine := hex.EncodeToString(sum[:]) + "  catchm-desktop-amd64-installer.exe\n"
	var corruptLine string
	if f.corrupt {
		bad := sha256.Sum256([]byte("different bytes entirely"))
		corruptLine = hex.EncodeToString(bad[:]) + "  catchm-desktop-amd64-installer.exe\n"
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			var assetList []string
			if !f.dropBin {
				assetList = append(assetList, fmt.Sprintf(`{"name":"catchm-desktop-amd64-installer.exe","browser_download_url":"http://%s/dl/installer.exe","size":%d}`, r.Host, len(f.installer)))
			}
			if !f.dropSum {
				line := sumLine
				if f.corrupt {
					line = corruptLine
				}
				assetList = append(assetList, fmt.Sprintf(`{"name":%q,"browser_download_url":"http://%s/dl/installer.exe.sha256","size":%d}`, ChecksumFileName, r.Host, len(line)))
			}
			fmt.Fprintf(w, `{"tag_name":"v9.9.9","body":"notes","html_url":"http://%s/rel","published_at":"2026-09-20T10:00:00Z","assets":[%s]}`,
				r.Host, strings.Join(assetList, ","))
		case "/dl/installer.exe":
			w.Write(f.installer)
		case "/dl/installer.exe.sha256":
			if f.corrupt {
				fmt.Fprint(w, corruptLine)
			} else {
				fmt.Fprint(w, sumLine)
			}
		default:
			http.NotFound(w, r)
		}
	}))
}

func releaseFor(t *testing.T, srv *httptest.Server) *Release {
	t.Helper()
	c := &GitHubClient{BaseURL: srv.URL, Client: srv.Client()}
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	return rel
}

func TestDownloadAndVerifySuccess(t *testing.T) {
	installer := []byte("this is definitely an NSIS installer \x00\x01\x02")
	srv := serveRelease(t, releaseFixture{installer: installer})
	defer srv.Close()

	c := &GitHubClient{BaseURL: srv.URL, Client: srv.Client()}
	got, err := DownloadAndVerify(context.Background(), c, releaseFor(t, srv))
	if err != nil {
		t.Fatalf("DownloadAndVerify: %v", err)
	}
	defer os.Remove(got.Path)

	data, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(installer) {
		t.Fatal("downloaded bytes differ from fixture")
	}
	wantSum := sha256.Sum256(installer)
	want := hex.EncodeToString(wantSum[:])
	if got.SHA256 != want {
		t.Fatalf("SHA256 = %s, want %s", got.SHA256, want)
	}
}

// The security-critical case: bytes arrived, checksum does not match —
// the temp file must be deleted and a *ChecksumMismatchError returned,
// so no caller can ever end up executing the corrupted download.
func TestDownloadAndVerifyCorruptedChecksumRejected(t *testing.T) {
	installer := []byte("pretend installer payload")
	srv := serveRelease(t, releaseFixture{installer: installer, corrupt: true})
	defer srv.Close()

	c := &GitHubClient{BaseURL: srv.URL, Client: srv.Client()}
	got, err := DownloadAndVerify(context.Background(), c, releaseFor(t, srv))
	var mism *ChecksumMismatchError
	if !errors.As(err, &mism) {
		t.Fatalf("error = %v, want *ChecksumMismatchError", err)
	}
	if mism.Missing || mism.Cause != nil {
		t.Fatalf("mismatch error carries wrong shape: %+v", mism)
	}
	if got.Path != "" {
		t.Fatal("verified installer returned despite mismatch")
	}
	if _, statErr := os.Stat(got.Path); got.Path != "" && statErr == nil {
		t.Fatalf("temp file %s survived a mismatch", got.Path)
	}
}

func TestDownloadAndVerifyMissingChecksumAsset(t *testing.T) {
	srv := serveRelease(t, releaseFixture{installer: []byte("x"), dropSum: true})
	defer srv.Close()

	c := &GitHubClient{BaseURL: srv.URL, Client: srv.Client()}
	_, err := DownloadAndVerify(context.Background(), c, releaseFor(t, srv))
	var mism *ChecksumMismatchError
	if !errors.As(err, &mism) || !mism.Missing {
		t.Fatalf("error = %v, want missing-checksum mismatch error", err)
	}
}

func TestDownloadAndVerifyMissingInstallerAsset(t *testing.T) {
	srv := serveRelease(t, releaseFixture{dropBin: true})
	defer srv.Close()

	c := &GitHubClient{BaseURL: srv.URL, Client: srv.Client()}
	if _, err := DownloadAndVerify(context.Background(), c, releaseFor(t, srv)); err == nil {
		t.Fatal("expected error for missing installer asset")
	}
}

// A mid-download network failure is an ordinary error (retryable), NOT
// a ChecksumMismatchError.
func TestDownloadAndVerifyNetworkFailureRetryable(t *testing.T) {
	srv := serveRelease(t, releaseFixture{installer: []byte("x")})
	srv.Close() // kill the server: asset download now fails at connect time

	c := &GitHubClient{BaseURL: srv.URL, Client: srv.Client()}
	rel := &Release{TagName: "v9.9.9", Assets: []Asset{
		{Name: "catchm-desktop-amd64-installer.exe", BrowserDownloadURL: srv.URL + "/dl/installer.exe"},
		{Name: ChecksumFileName, BrowserDownloadURL: srv.URL + "/dl/installer.exe.sha256"},
	}}
	_, err := DownloadAndVerify(context.Background(), c, rel)
	var mism *ChecksumMismatchError
	if errors.As(err, &mism) {
		t.Fatalf("network failure classified as checksum error: %v", err)
	}
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestCheckForUpdateEndToEnd(t *testing.T) {
	srv := serveRelease(t, releaseFixture{installer: []byte("x")})
	defer srv.Close()
	c := &GitHubClient{BaseURL: srv.URL, Client: srv.Client()}

	info, err := CheckForUpdate(context.Background(), c, "1.0.0")
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if info == nil || info.Version != "v9.9.9" || info.NotesURL == "" {
		t.Fatalf("info = %+v", info)
	}

	info, err = CheckForUpdate(context.Background(), c, "dev")
	if err != nil || info != nil {
		t.Fatalf("dev build: info=%v err=%v, want nil/nil", info, err)
	}

	info, err = CheckForUpdate(context.Background(), c, "9.9.9")
	if err != nil || info != nil {
		t.Fatalf("up-to-date: info=%v err=%v, want nil/nil", info, err)
	}
}

func TestParseChecksumFile(t *testing.T) {
	good := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  catchm-desktop-amd64-installer.exe\n" +
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff  other-file.zip\n"
	got, err := parseChecksumFile(good, "catchm-desktop-amd64-installer.exe")
	if err != nil || got != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("parseChecksumFile = %q, %v", got, err)
	}
	if _, err := parseChecksumFile(good, "absent.exe"); err == nil {
		t.Fatal("absent entry should error")
	}
	shortDigest := "abcd  catchm-desktop-amd64-installer.exe"
	if _, err := parseChecksumFile(shortDigest, "catchm-desktop-amd64-installer.exe"); err == nil {
		t.Fatal("short digest should error")
	}
}
