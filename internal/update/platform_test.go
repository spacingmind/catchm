package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fourAssetRelease carries every platform's assets, like a release
// the split CI (release.yml's windows + macos jobs) publishes.
func fourAssetRelease() *Release {
	return &Release{TagName: "v9.9.9", Assets: []Asset{
		{Name: "catchm-desktop-amd64-installer.exe", BrowserDownloadURL: "http://x/exe"},
		{Name: ChecksumFileName, BrowserDownloadURL: "http://x/exe.sha256"},
		{Name: "catchm-desktop-arm64.dmg", BrowserDownloadURL: "http://x/dmg"},
		{Name: "catchm-desktop-arm64.dmg.sha256", BrowserDownloadURL: "http://x/dmg.sha256"},
	}}
}

func TestPlatformAssets(t *testing.T) {
	cases := []struct {
		goos                string
		installer, checksum string
		ok                  bool
	}{
		{"windows", "catchm-desktop-amd64-installer.exe", "catchm-desktop-amd64-installer.exe.sha256", true},
		{"darwin", "catchm-desktop-arm64.dmg", "catchm-desktop-arm64.dmg.sha256", true},
		{"linux", "", "", false},
	}
	for _, c := range cases {
		ins, sum, ok := PlatformAssets(c.goos)
		if ins != c.installer || sum != c.checksum || ok != c.ok {
			t.Fatalf("PlatformAssets(%q) = %q, %q, %v; want %q, %q, %v", c.goos, ins, sum, ok, c.installer, c.checksum, c.ok)
		}
	}
}

func TestPlatformAssetSelection(t *testing.T) {
	rel := fourAssetRelease()
	for goos, want := range map[string]string{
		"windows": "http://x/exe",
		"darwin":  "http://x/dmg",
		"linux":   "",
	} {
		asset := PlatformAsset(rel, goos)
		if want == "" {
			if asset != nil {
				t.Fatalf("PlatformAsset(%q) = %+v, want nil", goos, asset)
			}
			continue
		}
		if asset == nil || asset.BrowserDownloadURL != want {
			t.Fatalf("PlatformAsset(%q) = %+v, want URL %s", goos, asset, want)
		}
	}
}

// CheckForUpdate gates on the platform's own asset: a newer release
// without it is not an available update for this client.
func TestCheckForUpdatePlatformGate(t *testing.T) {
	newRelease := func() *Release {
		return &Release{TagName: "v9.9.9", Assets: []Asset{
			{Name: "catchm-desktop-amd64-installer.exe", BrowserDownloadURL: "http://x/exe"},
			{Name: ChecksumFileName, BrowserDownloadURL: "http://x/exe.sha256"},
		}}
	}

	withGOOS(t, "windows")
	c := newLatestServer(t, newRelease())
	info, err := CheckForUpdate(context.Background(), c, "1.0.0")
	if err != nil || info == nil {
		t.Fatalf("windows: info = %v, err = %v (want an update)", info, err)
	}
	if info.DownloadURL != "http://x/exe" {
		t.Fatalf("windows DownloadURL = %q", info.DownloadURL)
	}

	withGOOS(t, "darwin")
	info, err = CheckForUpdate(context.Background(), newLatestServer(t, newRelease()), "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Fatalf("darwin saw a Windows-only release as an update: %+v", info)
	}

	withGOOS(t, "linux")
	info, err = CheckForUpdate(context.Background(), newLatestServer(t, fourAssetRelease()), "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Fatalf("linux saw an update despite no linux assets: %+v", info)
	}
}

// newLatestServer serves rel as /releases/latest — CheckForUpdate
// takes a concrete *ReleaseClient, not an interface.
func newLatestServer(t *testing.T, rel *Release) *ReleaseClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases/latest" {
			http.NotFound(w, r)
			return
		}
		b, err := json.Marshal(rel)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return &ReleaseClient{BaseURL: srv.URL, Client: srv.Client()}
}
