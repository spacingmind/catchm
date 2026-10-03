package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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

// ErrNoRelease means nothing has been published yet (the update
// Worker 404s /releases/latest in that case) — an expected condition
// on a fresh install, not a failure worth surfacing loudly.
var ErrNoRelease = errors.New("update: no releases published")

// HostGOOS is the OS whose release assets this process uses (and, in
// the desktop shell, whether it can self-install). A var so tests can
// pin it; never set it outside tests.
//
// a var (not a direct runtime.GOOS read) purely so tests can exercise
// the Windows asset path on any runner.
var HostGOOS = runtime.GOOS

// ChecksumFileName is the Windows installer's checksum asset name —
// kept for callers that need the name without a goos (tests, older
// call sites). Prefer PlatformAssets.
const ChecksumFileName = "catchm-desktop-amd64-installer.exe.sha256"

// PlatformAssets returns the asset names the release workflow
// publishes for an OS: its installer and the sha256sum-format checksum
// alongside it. ok is false for an OS with no release assets (Linux
// CI, any future platform before its assets ship).
func PlatformAssets(goos string) (installer, checksum string, ok bool) {
	switch goos {
	case "windows":
		return "catchm-desktop-amd64-installer.exe", "catchm-desktop-amd64-installer.exe.sha256", true
	case "darwin":
		return "catchm-desktop-arm64.dmg", "catchm-desktop-arm64.dmg.sha256", true
	default:
		return "", "", false
	}
}

// PlatformAsset returns the release's installer asset for an OS (nil
// when the release doesn't carry one — e.g. a release published before
// that platform's assets existed).
func PlatformAsset(rel *Release, goos string) *Asset {
	name, _, ok := PlatformAssets(goos)
	if !ok {
		return nil
	}
	return findAsset(rel, name)
}

// parseChecksumFile extracts the hex digest for name from a
// sha256sum-format file ("<sha256>  <name>" per line). The double
// space is what sha256sum writes for text-mode checksums; we accept
// any whitespace between the digest and the name, plus the optional
// leading "*" binary-mode marker coreutils puts on the filename (the
// MSYS/Git Bash sha256sum on the Windows release runner defaults to
// binary mode).
func parseChecksumFile(content, name string) (string, error) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.TrimPrefix(fields[1], "*") == name {
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
//   - errors.Is(err, context.Canceled): the caller cancelled ctx (the
//     user stopped the download) — not a failure, safe to retry.
//   - any other error: ordinary network/IO failure — safe to retry.
//
// Every error path removes the temp file, so a cancelled or failed
// download never leaves a partial installer behind.
func DownloadAndVerify(ctx context.Context, c *ReleaseClient, rel *Release) (out VerifiedInstaller, err error) {
	defer func() {
		// A cancel surfaces as whatever the in-flight read happened to
		// fail with — a wrapped transport error, or a Cause on a
		// checksum-fetch *ChecksumMismatchError. Normalise it, so a user
		// cancelling while the checksum downloads isn't treated as
		// possible tampering (the caller defers updates on those). A
		// real digest mismatch (Cause nil) is never rewritten.
		var mism *ChecksumMismatchError
		if err != nil && ctx.Err() != nil && (!errors.As(err, &mism) || mism.Cause != nil) {
			err = fmt.Errorf("update: download cancelled: %w", ctx.Err())
		}
	}()

	installerName, checksumName, ok := PlatformAssets(HostGOOS)
	if !ok {
		return out, fmt.Errorf("update: no release assets for GOOS %s", HostGOOS)
	}
	asset := findAsset(rel, installerName)
	if asset == nil {
		return out, fmt.Errorf("update: release %s has no %s asset", rel.TagName, installerName)
	}
	sumAsset := findAsset(rel, checksumName)
	if sumAsset == nil {
		return out, &ChecksumMismatchError{Asset: asset.Name, Missing: true}
	}

	// Installer → temp file, streaming.
	tmp, err := os.CreateTemp("", tempInstallerPattern)
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

// tempInstallerPattern names every installer DownloadAndVerify writes
// (os.CreateTemp pattern), so RemoveStaleInstallers only ever touches
// our own files.
const tempInstallerPattern = "catchm-update-*.exe"

// RemoveStaleInstallers deletes installers earlier runs left in the
// temp dir (downloaded, then deferred or never applied), except keep
// (the currently verified one; "" keeps none). Best-effort: returns
// the removals that failed — on Windows, e.g., an installer still
// running holds its file open.
//
// The caller must make sure no DownloadAndVerify is running: its
// partial file matches the same pattern.
func RemoveStaleInstallers(keep string) error {
	dir := os.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("update: list temp dir: %w", err)
	}
	var errs []error
	for _, e := range entries {
		// Match on the name alone, not filepath.Glob over the full
		// path: a temp dir containing glob metacharacters would
		// otherwise match nothing (or the wrong files).
		if ok, _ := filepath.Match(tempInstallerPattern, e.Name()); !ok || !e.Type().IsRegular() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if keep != "" && filepath.Clean(keep) == path {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
