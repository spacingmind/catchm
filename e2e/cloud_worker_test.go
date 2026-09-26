//go:build e2e

// Package e2e runs the real internal/update and internal/cloudsync
// clients against a live catchm-api Worker (wrangler dev, --local
// R2/D1) — the actual cross-repo contract, not an in-process fixture.
// It never runs under a plain `go test ./...` (see the build tag
// above) because it depends on another checkout (catchm-api)
// already running `wrangler dev` locally, and it shells out to that
// checkout's `wrangler` to mutate/restore its local R2 state along the
// way.
//
// Setup (separate terminal):
//
//	cd ../catchm-api && npx wrangler dev --port 8787
//
// local R2 must already have a seeded v9.9.9 release (latest.json +
// installer + matching .sha256) under the RELEASES bucket.
//
// Run:
//
//	go test -tags e2e ./e2e/...
package e2e

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/cloudsync"
	"github.com/spacingmind/catchm/internal/update"
)

func workerHost() string {
	if v := os.Getenv("E2E_WORKER_HOST"); v != "" {
		return v
	}
	return "127.0.0.1:8787"
}

func updateBaseURL() string { return "http://" + workerHost() + "/update" }
func cloudBaseURL() string  { return "http://" + workerHost() }

// cloudRepoDir locates the catchm-api checkout this test shells
// out to for local R2 mutation (wrangler r2 object get/put/delete
// --local) — a sibling of this repo's own checkout by default.
func cloudRepoDir(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("CATCHM_CLOUD_DIR"); v != "" {
		return v
	}
	dir := filepath.Join("..", "..", "catchm-api")
	if _, err := os.Stat(filepath.Join(dir, "wrangler.toml")); err != nil {
		t.Fatalf("catchm-api checkout not found at %s (set CATCHM_CLOUD_DIR): %v", dir, err)
	}
	return dir
}

func requireWorkerUp(t *testing.T) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", workerHost(), 500*time.Millisecond)
	if err != nil {
		t.Skipf("wrangler dev not reachable on %s (%v) — start it: cd ../catchm-api && npx wrangler dev --port 8787", workerHost(), err)
	}
	conn.Close()
}

// r2Local runs `wrangler r2 object <verb> ...` against the catchm-api
// checkout's LOCAL persisted state (never --remote): this is the same
// on-disk state the running `wrangler dev` reads from, so a put here
// is visible to the next HTTP request with no restart needed.
func r2Local(t *testing.T, args ...string) []byte {
	t.Helper()
	dir := cloudRepoDir(t)
	full := append([]string{"wrangler", "r2", "object"}, args...)
	full = append(full, "--local")
	cmd := exec.Command("npx", full...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("npx %s (in %s): %v\n%s", strings.Join(full, " "), dir, err, out)
	}
	return out
}

// backupAndRestore snapshots key to a temp file via `wrangler r2
// object get --local` and registers a cleanup that puts it straight
// back, byte-for-byte, regardless of what the test does to it.
func backupAndRestore(t *testing.T, key string) {
	t.Helper()
	backup := filepath.Join(t.TempDir(), "backup")
	r2Local(t, "get", key, "--file", backup)
	t.Cleanup(func() {
		r2Local(t, "put", key, "--file", backup)
	})
}

func signup(t *testing.T, c *cloudsync.Client, email string) string {
	t.Helper()
	token, _, err := c.Signup(cloudsync.SignupRequest{
		Email:             email,
		AuthHash:          "hash-" + email,
		AuthSalt:          "salt-" + email,
		KDFParams:         `{"time":2}`,
		WrappedMasterKey:  "wmk-" + email,
		KeyWrapParams:     `{"time":2}`,
		RecoverySalt:      "rsalt-" + email,
		RecoveryAuthHash:  "rhash-" + email,
		RecoveryBlob:      "rb-" + email,
		RecoveryKDFParams: `{"time":2}`,
	})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	return token
}

func uniqueEmail(tag string) string {
	return fmt.Sprintf("e2e-%s-%d@example.com", tag, time.Now().UnixNano())
}

// --- Part A: update endpoints, against the real Worker ---

func TestUpdateCheckForUpdateAvailableAndUpToDate(t *testing.T) {
	requireWorkerUp(t)
	c := update.NewReleaseClient(updateBaseURL())

	info, err := update.CheckForUpdate(context.Background(), c, "1.0.0")
	if err != nil {
		t.Fatalf("CheckForUpdate(1.0.0): %v", err)
	}
	if info == nil || info.Version != "v9.9.9" {
		t.Fatalf("info = %+v, want v9.9.9 available", info)
	}

	info, err = update.CheckForUpdate(context.Background(), c, "9.9.9")
	if err != nil {
		t.Fatalf("CheckForUpdate(9.9.9): %v", err)
	}
	if info != nil {
		t.Fatalf("info = %+v, want up to date (nil)", info)
	}
}

func TestUpdateDownloadAndVerifySuccess(t *testing.T) {
	requireWorkerUp(t)
	c := update.NewReleaseClient(updateBaseURL())

	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	verified, err := update.DownloadAndVerify(context.Background(), c, rel)
	if err != nil {
		t.Fatalf("DownloadAndVerify: %v", err)
	}
	defer os.Remove(verified.Path)

	data, err := os.ReadFile(verified.Path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != verified.SHA256 {
		t.Fatalf("SHA256 = %s, recomputed = %x", verified.SHA256, sum)
	}
}

func TestUpdateChecksumMismatchRejectedNoTempFileLeftBehind(t *testing.T) {
	requireWorkerUp(t)
	const key = "catchm-releases/v9.9.9/catchm-desktop-amd64-installer.exe.sha256"
	backupAndRestore(t, key)

	wrongFile := filepath.Join(t.TempDir(), "wrong.sha256")
	wrongLine := strings.Repeat("0", 64) + "  catchm-desktop-amd64-installer.exe\n"
	if err := os.WriteFile(wrongFile, []byte(wrongLine), 0o644); err != nil {
		t.Fatal(err)
	}
	r2Local(t, "put", key, "--file", wrongFile)

	before, err := filepath.Glob(filepath.Join(os.TempDir(), "catchm-update-*"))
	if err != nil {
		t.Fatal(err)
	}

	c := update.NewReleaseClient(updateBaseURL())
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	verified, err := update.DownloadAndVerify(context.Background(), c, rel)
	var mism *update.ChecksumMismatchError
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

func TestUpdateNoReleaseYetIsErrNoRelease(t *testing.T) {
	requireWorkerUp(t)
	const key = "catchm-releases/latest.json"
	backupAndRestore(t, key)

	r2Local(t, "delete", key)

	c := update.NewReleaseClient(updateBaseURL())
	if _, err := c.Latest(context.Background()); !errors.Is(err, update.ErrNoRelease) {
		t.Fatalf("Latest error = %v, want ErrNoRelease", err)
	}
	if _, err := update.CheckForUpdate(context.Background(), c, "1.0.0"); !errors.Is(err, update.ErrNoRelease) {
		t.Fatalf("CheckForUpdate error = %v, want ErrNoRelease", err)
	}
}

// --- Parts A/B: cloudsync, against the real Worker ---

func TestCloudSyncPutGetRoundTrip(t *testing.T) {
	requireWorkerUp(t)
	c := cloudsync.NewClient(cloudBaseURL())
	token := signup(t, c, uniqueEmail("roundtrip"))

	plaintext := make([]byte, 4096)
	if _, err := rand.Read(plaintext); err != nil {
		t.Fatal(err)
	}
	version, conflict, err := c.PutBlob(token, "meet-1", plaintext, 0)
	if err != nil || conflict != nil {
		t.Fatalf("PutBlob: err=%v conflict=%+v", err, conflict)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1", version)
	}

	got, gotVersion, err := c.GetBlob(token, "meet-1")
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	if string(got) != string(plaintext) || gotVersion != 1 {
		t.Fatalf("GetBlob returned %d bytes at version %d", len(got), gotVersion)
	}
}

func TestCloudSyncPutBlobOverCapIsRejected(t *testing.T) {
	requireWorkerUp(t)
	c := cloudsync.NewClient(cloudBaseURL())
	token := signup(t, c, uniqueEmail("overcap"))

	// MAX_BLOB_BYTES is 5 MiB (wrangler.toml); one byte over it.
	oversized := make([]byte, 5*1024*1024+1)
	if _, err := rand.Read(oversized); err != nil {
		t.Fatal(err)
	}
	_, conflict, err := c.PutBlob(token, "too-big", oversized, 0)
	if !errors.Is(err, cloudsync.ErrBlobTooLarge) {
		t.Fatalf("err = %v (conflict=%+v), want ErrBlobTooLarge", err, conflict)
	}
}

// TestCloudSyncListBlobsPaginatesAcrossRealServerPages exercises the
// actual next_cursor pagination end to end. internal/cloudsync.Client
// has no knob to shrink the server's page size (DEFAULT_LIST_LIMIT is
// 500, hardcoded server-side — see catchm-api's listBlobs), so
// the only way to force the real Worker across more than one page
// through the real client is to actually exceed that limit: this pushes
// 501 blobs into a fresh account and asserts ListBlobs's cursor-following
// loop (internal/cloudsync/client.go) returns every one of them, in
// ascending id order, deduplicated. (internal/cloudsync/client_test.go
// already covers the cursor-following logic itself against a mock
// server with a forced page size of 1 — this test's job is only to
// confirm the real server's envelope and ordering match what that
// logic expects.)
func TestCloudSyncListBlobsPaginatesAcrossRealServerPages(t *testing.T) {
	requireWorkerUp(t)
	if testing.Short() {
		t.Skip("short mode: skipping the 501-blob pagination push")
	}
	c := cloudsync.NewClient(cloudBaseURL())
	token := signup(t, c, uniqueEmail("pagination"))

	const total = 501 // one past DEFAULT_LIST_LIMIT: forces a real second page
	want := make(map[string]bool, total)
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("blob-%04d", i)
		want[id] = true
		if _, conflict, err := c.PutBlob(token, id, []byte(id), 0); err != nil || conflict != nil {
			t.Fatalf("PutBlob(%s): err=%v conflict=%+v", id, err, conflict)
		}
	}

	metas, err := c.ListBlobs(token)
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	if len(metas) != total {
		t.Fatalf("got %d blobs, want %d", len(metas), total)
	}
	seen := make(map[string]bool, total)
	for _, m := range metas {
		if seen[m.ID] {
			t.Fatalf("duplicate blob %q across pages", m.ID)
		}
		seen[m.ID] = true
		if !want[m.ID] {
			t.Fatalf("unexpected blob %q", m.ID)
		}
	}
}
