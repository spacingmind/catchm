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
}

// CheckForUpdate fetches the latest release and returns an UpdateInfo
// if it is newer than current. nil UpdateInfo means up to date (or
// current is a dev build — never nag a dev build, see IsNewer).
func CheckForUpdate(ctx context.Context, c *GitHubClient, current string) (*UpdateInfo, error) {
	rel, err := c.Latest(ctx)
	if err != nil {
		return nil, err
	}
	if !IsNewer(current, rel.TagName) {
		return nil, nil
	}
	return &UpdateInfo{
		Version:     rel.TagName,
		Notes:       rel.Body,
		NotesURL:    rel.HTMLURL,
		PublishedAt: rel.PublishedAt,
	}, nil
}
