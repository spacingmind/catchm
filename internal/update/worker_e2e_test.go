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
	"path/filepath"
	"testing"
)

// workerFixture mimics the catchm-cloud update Worker exactly (Part
// A of docs/plans/completed/m24-cloud-update-endpoint.md): the same two
// routes under /update, absolute browser_download_url values built
// from the request's own origin, and a 404 with no manifest.
type workerFixture struct {
	installer []byte
	corrupt   bool // publish a checksum that does NOT match
	noRelease bool // 404 the latest endpoint, as on a fresh deploy
}

func newWorkerServer(t *testing.T, f workerFixture) *httptest.Server {
	t.Helper()
	sum := sha256.Sum256(f.installer)
	sumLine := hex.EncodeToString(sum[:]) + "  catchm-desktop-amd64-installer.exe\n"
	if f.corrupt {
		bad := sha256.Sum256([]byte("tampered"))
		sumLine = hex.EncodeToString(bad[:]) + "  catchm-desktop-amd64-installer.exe\n"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/update/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if f.noRelease {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":"no release"}`)
			return
		}
		origin := "http://" + r.Host
		fmt.Fprintf(w, `{
			"tag_name": "v9.9.9",
			"name": "CatchM v9.9.9",
			"body": "release notes",
			"draft": false,
			"prerelease": false,
			"html_url": "",
			"published_at": "2026-09-20T10:00:00Z",
			"assets": [
				{"name": "catchm-desktop-amd64-installer.exe", "browser_download_url": %q, "size": %d},
				{"name": %q, "browser_download_url": %q, "size": %d}
			]
		}`,
			origin+"/update/download/v9.9.9/catchm-desktop-amd64-installer.exe", len(f.installer),
			ChecksumFileName, origin+"/update/download/v9.9.9/"+ChecksumFileName, len(sumLine))
	})
	mux.HandleFunc("/update/download/v9.9.9/catchm-desktop-amd64-installer.exe", func(w http.ResponseWriter, r *http.Request) {
		w.Write(f.installer)
	})
	mux.HandleFunc("/update/download/v9.9.9/"+ChecksumFileName, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sumLine)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// withGOOS pins HostGOOS for the test and restores it after.
func withGOOS(t *testing.T, goos string) {
	t.Helper()
	old := HostGOOS
	HostGOOS = goos
	t.Cleanup(func() { HostGOOS = old })
}

func TestWorkerEndToEndSuccess(t *testing.T) {
	withGOOS(t, "windows")
	installer := []byte("this is definitely an NSIS installer \x00\x01\x02")
	srv := newWorkerServer(t, workerFixture{installer: installer})
	c := &ReleaseClient{BaseURL: srv.URL + "/update", Client: srv.Client()}

	info, err := CheckForUpdate(context.Background(), c, "1.0.0")
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if info == nil || info.Version != "v9.9.9" {
		t.Fatalf("info = %+v", info)
	}

	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	verified, err := DownloadAndVerify(context.Background(), c, rel)
	if err != nil {
		t.Fatalf("DownloadAndVerify: %v", err)
	}
	defer os.Remove(verified.Path)

	data, err := os.ReadFile(verified.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(installer) {
		t.Fatal("downloaded bytes differ from fixture")
	}
	wantSum := sha256.Sum256(installer)
	if verified.SHA256 != hex.EncodeToString(wantSum[:]) {
		t.Fatalf("SHA256 = %s", verified.SHA256)
	}
}

func TestWorkerNoReleaseYet(t *testing.T) {
	srv := newWorkerServer(t, workerFixture{noRelease: true})
	c := &ReleaseClient{BaseURL: srv.URL + "/update", Client: srv.Client()}

	if _, err := c.Latest(context.Background()); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("Latest error = %v, want ErrNoRelease", err)
	}
	if _, err := CheckForUpdate(context.Background(), c, "1.0.0"); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("CheckForUpdate error = %v, want ErrNoRelease", err)
	}
}

func TestWorkerWrongChecksumLeavesNoTempFile(t *testing.T) {
	withGOOS(t, "windows")
	srv := newWorkerServer(t, workerFixture{installer: []byte("pretend installer payload"), corrupt: true})
	c := &ReleaseClient{BaseURL: srv.URL + "/update", Client: srv.Client()}

	before, err := filepath.Glob(filepath.Join(os.TempDir(), "catchm-update-*"))
	if err != nil {
		t.Fatal(err)
	}

	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	verified, err := DownloadAndVerify(context.Background(), c, rel)
	var mism *ChecksumMismatchError
	if !errors.As(err, &mism) {
		t.Fatalf("error = %v, want *ChecksumMismatchError", err)
	}
	if verified.Path != "" {
		t.Fatal("verified installer returned despite mismatch")
	}

	after, err := filepath.Glob(filepath.Join(os.TempDir(), "catchm-update-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(after) > len(before) {
		t.Fatalf("leftover catchm-update-* temp file(s): before=%v after=%v", before, after)
	}
}
