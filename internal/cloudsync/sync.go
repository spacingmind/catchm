package cloudsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spacingmind/catchm/internal/meeting"
)

// Meeting file names, mirroring internal/meeting's on-disk layout.
const (
	docFile        = "meeting.md"
	transcriptFile = "utterances.jsonl"
	batchFile      = "utterances.v2.jsonl"
)

// stateFileName is the sync manifest, stored next to config.yaml under
// CATCHM_HOME. It holds only versions and content checksums — no
// secrets — so plaintext storage is fine.
//
// The manifest is keyed per server (cloud-sync-state-<sha256(url)[:16]>
// .json): remote versions and pushed checksums belong to exactly one
// server, so switching A→B→A must neither push against B with A's
// versions nor forget A's state. Sync reads the URL from the config
// the caller saved; legacy unkeyed files are ignored (pre-launch dev
// data only — recorded in M18 Decisions).
const stateFileName = "cloud-sync-state.json"

type syncStateEntry struct {
	RemoteVersion  int    `json:"remote_version"`
	PushedChecksum string `json:"pushed_checksum"`
}

type syncState map[string]syncStateEntry

// SyncResult summarizes one sync run for the UI (M13d).
type SyncResult struct {
	Pushed     []string `json:"pushed"`
	Pulled     []string `json:"pulled"`
	Conflicted []string `json:"conflicted"`
}

// Sync pushes every locally-changed meeting, reconciling conflicts per
// the LWW-plus-conflict-copy design, then pulls down remote meetings
// this device has never seen. The manifest is only saved after a fully
// successful run; a mid-sync error leaves it untouched, and the next
// run re-attempts the affected pushes through the same conflict paths.
//
// Checksums: the manifest's PushedChecksum is the sha256 of the
// marshaled plaintext bundle (deterministic — sorted file order), not
// of the ciphertext, whose fresh random nonce would make every
// encryption differ. The ciphertext checksum from
// EncryptMeetingBundle is only the transport check for X-Checksum.
func Sync(store *meeting.Store, client *Client, sessionToken string, masterKey []byte) (*SyncResult, error) {
	path, err := statePath(client.BaseURL())
	if err != nil {
		return nil, err
	}
	state, err := loadState(path)
	if err != nil {
		return nil, err
	}
	result := &SyncResult{}

	meetings, err := store.List()
	if err != nil {
		return nil, err
	}
	localIDs := make(map[string]bool, len(meetings))
	for _, m := range meetings {
		localIDs[m.ID] = true

		files, err := readMeetingFiles(store.Root(), m.ID)
		if err != nil {
			return nil, fmt.Errorf("read meeting %s: %w", m.ID, err)
		}
		checksum := contentChecksum(files)
		entry := state[m.ID]
		if entry.RemoteVersion > 0 && entry.PushedChecksum == checksum {
			continue // unchanged since the last push/adoption
		}

		ciphertext, _, err := EncryptMeetingBundle(masterKey, m.ID, files)
		if err != nil {
			return nil, fmt.Errorf("encrypt meeting %s: %w", m.ID, err)
		}
		newVersion, conflict, err := client.PutBlob(sessionToken, m.ID, ciphertext, entry.RemoteVersion)
		if err != nil {
			return nil, fmt.Errorf("push meeting %s: %w", m.ID, err)
		}
		if conflict != nil {
			if conflict.Version == 0 {
				// We expected a version but the server has no row
				// (e.g. local state references a server-side
				// deletion). Re-push as a brand-new blob.
				newVersion, conflict, err = client.PutBlob(sessionToken, m.ID, ciphertext, 0)
				if err != nil {
					return nil, fmt.Errorf("re-push meeting %s: %w", m.ID, err)
				}
				if conflict != nil {
					return nil, fmt.Errorf("push meeting %s: unexpected conflict on fresh blob: %+v", m.ID, conflict)
				}
				state[m.ID] = syncStateEntry{RemoteVersion: newVersion, PushedChecksum: checksum}
				result.Pushed = append(result.Pushed, m.ID)
				continue
			}
			if err := adoptRemote(store, client, sessionToken, masterKey, m.ID, state, result); err != nil {
				return nil, fmt.Errorf("conflict on meeting %s: %w", m.ID, err)
			}
			continue
		}
		state[m.ID] = syncStateEntry{RemoteVersion: newVersion, PushedChecksum: checksum}
		result.Pushed = append(result.Pushed, m.ID)
	}

	metas, err := client.ListBlobs(sessionToken)
	if err != nil {
		return nil, fmt.Errorf("list blobs: %w", err)
	}
	for _, meta := range metas {
		entry := state[meta.ID]

		// A remote meeting this device has never seen: pull it in.
		if !localIDs[meta.ID] {
			ciphertext, _, err := client.GetBlob(sessionToken, meta.ID)
			if err != nil {
				return nil, fmt.Errorf("fetch meeting %s: %w", meta.ID, err)
			}
			files, err := DecryptMeetingBundle(masterKey, meta.ID, ciphertext)
			if err != nil {
				return nil, fmt.Errorf("decrypt meeting %s: %w", meta.ID, err)
			}
			if err := materializeMeeting(store, meta.ID, files); err != nil {
				return nil, fmt.Errorf("materialize meeting %s: %w", meta.ID, err)
			}
			state[meta.ID] = syncStateEntry{RemoteVersion: meta.Version, PushedChecksum: contentChecksum(files)}
			result.Pulled = append(result.Pulled, meta.ID)
			continue
		}

		// A local meeting whose remote version advanced while this
		// device was away. If local content diverged too, the push
		// phase above already pushed or conflicted and refreshed the
		// manifest entry — so reaching here with a diverged local copy
		// means an unusual edge; fall back to the conflict path
		// rather than overwriting divergent local content silently.
		if entry.RemoteVersion >= meta.Version {
			continue
		}
		files, err := readMeetingFiles(store.Root(), meta.ID)
		if err != nil {
			return nil, fmt.Errorf("read meeting %s: %w", meta.ID, err)
		}
		if entry.PushedChecksum != contentChecksum(files) {
			if err := adoptRemote(store, client, sessionToken, masterKey, meta.ID, state, result); err != nil {
				return nil, fmt.Errorf("conflict on meeting %s: %w", meta.ID, err)
			}
			continue
		}
		ciphertext, _, err := client.GetBlob(sessionToken, meta.ID)
		if err != nil {
			return nil, fmt.Errorf("fetch meeting %s: %w", meta.ID, err)
		}
		files, err = DecryptMeetingBundle(masterKey, meta.ID, ciphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt meeting %s: %w", meta.ID, err)
		}
		if err := materializeMeeting(store, meta.ID, files); err != nil {
			return nil, fmt.Errorf("materialize meeting %s: %w", meta.ID, err)
		}
		state[meta.ID] = syncStateEntry{RemoteVersion: meta.Version, PushedChecksum: contentChecksum(files)}
		result.Pulled = append(result.Pulled, meta.ID)
	}

	if err := saveState(path, state); err != nil {
		return nil, err
	}
	return result, nil
}

// adoptRemote handles a lost push race: the local divergent files are
// preserved as conflict-<timestamp> copies inside the meeting's own
// directory (a signal for the user to reconcile manually — no
// automatic merging), then the remote content becomes the local truth
// and the manifest moves to the remote version.
func adoptRemote(store *meeting.Store, client *Client, sessionToken string, masterKey []byte, id string, state syncState, result *SyncResult) error {
	if !validMeetingID(id) {
		return fmt.Errorf("cloudsync: invalid meeting id %q", id)
	}
	ciphertext, remoteVersion, err := client.GetBlob(sessionToken, id)
	if err != nil {
		return err
	}
	files, err := DecryptMeetingBundle(masterKey, id, ciphertext)
	if err != nil {
		return err
	}

	dir := filepath.Join(store.Root(), id)
	ts := time.Now().UnixMilli()
	// The v2 copy isn't in the spec's .md/.jsonl pair naming, but
	// dropping a locally-diverged batch transcript would be silent
	// data loss.
	for _, pair := range [][2]string{
		{docFile, fmt.Sprintf("conflict-%d.md", ts)},
		{transcriptFile, fmt.Sprintf("conflict-%d.jsonl", ts)},
		{batchFile, fmt.Sprintf("conflict-%d.v2.jsonl", ts)},
	} {
		if data, err := os.ReadFile(filepath.Join(dir, pair[0])); err == nil {
			if err := os.WriteFile(filepath.Join(dir, pair[1]), data, 0o600); err != nil {
				return err
			}
		}
	}

	if err := materializeMeeting(store, id, files); err != nil {
		return err
	}
	state[id] = syncStateEntry{RemoteVersion: remoteVersion, PushedChecksum: contentChecksum(files)}
	result.Conflicted = append(result.Conflicted, id)
	return nil
}

// materializeMeeting writes a decrypted remote bundle into the store
// as local files, creating the meeting directory if needed (the
// pull-a-new-meeting path).
func materializeMeeting(store *meeting.Store, id string, files map[string][]byte) error {
	if !validMeetingID(id) {
		return fmt.Errorf("cloudsync: invalid meeting id %q", id)
	}
	dir := filepath.Join(store.Root(), id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, name := range sortedFileNames(files) {
		if name != docFile && name != transcriptFile && name != batchFile {
			return fmt.Errorf("cloudsync: unexpected file %q in bundle", name)
		}
		if err := os.WriteFile(filepath.Join(dir, name), files[name], 0o600); err != nil {
			return err
		}
	}
	return nil
}

// validMeetingID mirrors internal/meeting's unexported validID: blob
// ids become directory names via store.Root(), so separators and
// traversal must be rejected here too.
func validMeetingID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, `/\`)
}

// readMeetingFiles loads the files a sync bundles for one meeting:
// meeting.md always; utterances.jsonl always (a meeting started but
// never spoken into has no transcript file yet — bundled as empty
// content so the bundle shape stays stable); utterances.v2.jsonl when
// it exists. A missing meeting.md is an error — the store's List only
// returns meetings whose doc parses, so a doc that vanished between
// List and read is a genuine anomaly, not a normal state.
func readMeetingFiles(root, id string) (map[string][]byte, error) {
	dir := filepath.Join(root, id)
	files := map[string][]byte{}
	data, err := os.ReadFile(filepath.Join(dir, docFile))
	if err != nil {
		return nil, err
	}
	files[docFile] = data
	for _, name := range []string{transcriptFile, batchFile} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if os.IsNotExist(err) {
			if name == transcriptFile {
				files[transcriptFile] = []byte{}
				continue
			}
			continue // v2 transcript is optional
		}
		if err != nil {
			return nil, err
		}
		files[name] = data
	}
	return files, nil
}

// contentChecksum hashes the marshaled plaintext bundle, the stable
// identity of a meeting's content independent of encryption nonce.
func contentChecksum(files map[string][]byte) string {
	sum := sha256.Sum256(marshalBundle(files))
	return hex.EncodeToString(sum[:])
}

// statePath resolves the per-server manifest path. serverURL is the
// normalized cloud base URL the client talks to; the empty string
// falls back to the legacy unkeyed name (kept readable so a manual
// inspection of an old install still works).
func statePath(serverURL string) (string, error) {
	home := os.Getenv("CATCHM_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(userHome, ".catchm")
	}
	if serverURL == "" {
		return filepath.Join(home, stateFileName), nil
	}
	sum := sha256.Sum256([]byte(serverURL))
	key := hex.EncodeToString(sum[:])[:16]
	return filepath.Join(home, fmt.Sprintf("cloud-sync-state-%s.json", key)), nil
}

func loadState(path string) (syncState, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return syncState{}, nil
	}
	if err != nil {
		return nil, err
	}
	state := syncState{}
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return state, nil
}

func saveState(path string, state syncState) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}
