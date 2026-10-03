package update

import (
	"context"
	"time"
)

// UpdateInfo is the result of a successful check that found a newer
// release. Plain struct — no Wails types in this package.
type UpdateInfo struct {
	Version     string
	Notes       string
	NotesURL    string
	PublishedAt time.Time
	// DownloadURL is the platform installer asset's URL, for clients
	// that can't self-install (the macOS dmg).
	DownloadURL string
}

// CheckForUpdate fetches the latest release and returns an UpdateInfo
// if it is newer than current. nil UpdateInfo means up to date (or
// current is a dev build — never nag a dev build, see IsNewer).
func CheckForUpdate(ctx context.Context, c *ReleaseClient, current string) (*UpdateInfo, error) {
	rel, err := c.Latest(ctx)
	if err != nil {
		return nil, err
	}
	if !IsNewer(current, rel.TagName) {
		return nil, nil
	}
	// A newer release without this platform's installer asset is not
	// an available update: nothing here could be installed, so a Mac
	// client (say) must not report a Windows-only release.
	asset := PlatformAsset(rel, HostGOOS)
	if asset == nil {
		return nil, nil
	}
	return &UpdateInfo{
		Version:     rel.TagName,
		Notes:       rel.Body,
		NotesURL:    rel.HTMLURL,
		PublishedAt: rel.PublishedAt,
		DownloadURL: asset.BrowserDownloadURL,
	}, nil
}
