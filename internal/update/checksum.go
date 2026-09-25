package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ChecksumMismatchError means bytes were downloaded successfully but
// their SHA-256 does not match the release's published checksum. The
// caller must NEVER execute the downloaded file in this case, and
// retrying the download only makes sense if the *published* checksum
// is trusted to be the good one (i.e. treat as possible corruption or
// tampering — safest is to surface it, not auto-retry).
type ChecksumMismatchError struct {
	Asset   string
	Want    string
	Got     string
	Cause   error // network/decode failure while fetching the checksum itself
	Missing bool  // release published no checksum asset at all
}

func (e *ChecksumMismatchError) Error() string {
	switch {
	case e.Missing:
		return fmt.Sprintf("update: release %q has no checksum asset", e.Asset)
	case e.Cause != nil:
		return fmt.Sprintf("update: fetch checksum for %q: %v", e.Asset, e.Cause)
	default:
		return fmt.Sprintf("update: checksum mismatch for %q: want %s, got %s — not executing this file", e.Asset, e.Want, e.Got)
	}
}

// ErrNoRelease means the repo has no releases yet (GitHub 404s
// /releases/latest in that case) — an expected condition on a fresh
// repo, not a failure worth surfacing loudly.
var ErrNoRelease = errors.New("update: no releases published")

// ChecksumFileName is the asset name the release workflow publishes
// alongside the installer (sha256sum-compatible format).
const ChecksumFileName = "catchm-desktop-amd64-installer.exe.sha256"

// parseChecksumFile extracts the hex digest for name from a
// sha256sum-format file ("<sha256>  <name>" per line). The double
// space is what sha256sum writes for text-mode checksums; we accept
// any whitespace between the digest and the name.
func parseChecksumFile(content, name string) (string, error) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == name {
			digest := fields[0]
			if len(digest) != sha256.Size*2 {
				return "", fmt.Errorf("digest for %q is %d hex chars, want %d", name, len(digest), sha256.Size*2)
			}
			return strings.ToLower(digest), nil
		}
	}
	return "", fmt.Errorf("no entry for %q in checksum file", name)
}

// findAsset returns the release asset whose name matches the
// installer or checksum filename.
func findAsset(rel *Release, name string) *Asset {
	for i := range rel.Assets {
		if rel.Assets[i].Name == name {
			return &rel.Assets[i]
		}
	}
	return nil
}

// DownloadAndVerify downloads the release's installer asset to a temp
// file (streaming via io.Copy — installers are tens of MB and must not
// be buffered whole in memory) and verifies its SHA-256 against the
// checksum asset published in the same release, BEFORE returning the
// path. The returned file has already been closed; it is the caller's
// job to remove it after use.
//
// Error taxonomy the caller needs to tell apart:
//   - *ChecksumMismatchError: never execute anything; do not blind-retry.
//   - any other error: ordinary network/IO failure — safe to retry.
func DownloadAndVerify(ctx context.Context, c *GitHubClient, rel *Release) (VerifiedInstaller, error) {
	out := VerifiedInstaller{}

	asset := findAsset(rel, "catchm-desktop-amd64-installer.exe")
	if asset == nil {
		return out, fmt.Errorf("update: release %s has no installer asset", rel.TagName)
	}
	sumAsset := findAsset(rel, ChecksumFileName)
	if sumAsset == nil {
		return out, &ChecksumMismatchError{Asset: asset.Name, Missing: true}
	}

	// Installer → temp file, streaming.
	tmp, err := os.CreateTemp("", "catchm-update-*.exe")
	if err != nil {
		return out, fmt.Errorf("update: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	resp, err := c.Download(ctx, asset.BrowserDownloadURL)
	if err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return out, err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		tmp.Close()
		resp.Body.Close()
		os.Remove(tmpPath)
		return out, fmt.Errorf("update: download installer: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return out, fmt.Errorf("update: close temp installer: %w", err)
	}
	resp.Body.Close()
	got := hex.EncodeToString(h.Sum(nil))

	// Checksum asset → expected digest.
	sumResp, err := c.Download(ctx, sumAsset.BrowserDownloadURL)
	if err != nil {
		os.Remove(tmpPath)
		return out, &ChecksumMismatchError{Asset: asset.Name, Cause: err}
	}
	sumBytes, err := io.ReadAll(io.LimitReader(sumResp.Body, 1<<20))
	sumResp.Body.Close()
	if err != nil {
		os.Remove(tmpPath)
		return out, &ChecksumMismatchError{Asset: asset.Name, Cause: err}
	}
	want, perr := parseChecksumFile(string(sumBytes), asset.Name)
	if perr != nil {
		os.Remove(tmpPath)
		return out, &ChecksumMismatchError{Asset: asset.Name, Cause: perr}
	}
	if !strings.EqualFold(want, got) {
		os.Remove(tmpPath)
		return out, &ChecksumMismatchError{Asset: asset.Name, Want: want, Got: got}
	}

	out.Path = tmpPath
	out.SHA256 = got
	return out, nil
}

// VerifiedInstaller is a downloaded, checksum-verified installer
// ready to be executed.
type VerifiedInstaller struct {
	Path   string
	SHA256 string
}

// String makes the temp path obvious in logs.
func (v VerifiedInstaller) String() string {
	return filepath.Base(v.Path) + " (sha256:" + v.SHA256[:12] + "…)"
}
