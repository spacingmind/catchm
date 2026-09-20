package cloudsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/e2ee"
	"github.com/spacingmind/catchm/internal/meeting"
)

// newSyncFixture builds two independent Stores (two devices) against
// one shared fake Worker, with CATCHM_HOME isolated per device (each
// device has its own sync manifest).
type syncFixture struct {
	t            *testing.T
	server       *fakeWorker
	client       *Client
	token        string
	masterKey    []byte
	storeA       *meeting.Store
	storeB       *meeting.Store
	homeA, homeB string
}

func newSyncFixture(t *testing.T) *syncFixture {
	t.Helper()
	server := httptestNewServer(t, newFakeWorker())
	client := NewClient(server.URL)
	_ = server
	token := httptestSignup(t, client, "sync@example.com")

	homeA, homeB := t.TempDir(), t.TempDir()
	t.Setenv("CATCHM_HOME", homeA) // default for helper functions run before explicit switches
	storeA := newStoreAt(t, homeA)
	storeB := newStoreAt(t, homeB)

	return &syncFixture{
		t: t, client: client, token: token,
		masterKey: e2ee.GenerateMasterKey(),
		storeA:    storeA, storeB: storeB,
		homeA: homeA, homeB: homeB,
	}
}

// syncA/syncB run Sync for one device under that device's CATCHM_HOME.
func (f *syncFixture) syncA() (*SyncResult, error) {
	f.t.Setenv("CATCHM_HOME", f.homeA)
	return Sync(f.storeA, f.client, f.token, f.masterKey)
}

func (f *syncFixture) syncB() (*SyncResult, error) {
	f.t.Setenv("CATCHM_HOME", f.homeB)
	return Sync(f.storeB, f.client, f.token, f.masterKey)
}

// createMeeting makes a local meeting with one transcript utterance.
func createMeeting(t *testing.T, store *meeting.Store, title string) *meeting.Meeting {
	t.Helper()
	m, err := store.Start(title, time.Now().UTC(), meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(m.ID, meeting.Utterance{Text: "first utterance"}); err != nil {
		t.Fatal(err)
	}
	return m
}

func readMeetingFile(t *testing.T, store *meeting.Store, id, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(store.Root(), id, name))
	if err != nil {
		t.Fatalf("read %s/%s: %v", id, name, err)
	}
	return string(raw)
}

func TestSyncPushesNewMeeting(t *testing.T) {
	f := newSyncFixture(t)
	m := createMeeting(t, f.storeA, "Push me")

	result, err := f.syncA()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Pushed) != 1 || result.Pushed[0] != m.ID {
		t.Fatalf("pushed = %v, want [%s]", result.Pushed, m.ID)
	}
	if len(result.Pulled) != 0 || len(result.Conflicted) != 0 {
		t.Fatalf("pulled=%v conflicted=%v, want empty", result.Pulled, result.Conflicted)
	}

	if _, _, err := f.client.GetBlob(f.token, m.ID); err != nil {
		t.Fatalf("blob missing server-side after push: %v", err)
	}

	// Second sync of the same unchanged meeting is a no-op.
	result, err = f.syncA()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Pushed) != 0 {
		t.Fatalf("unchanged meeting re-pushed: %v", result.Pushed)
	}
}

func TestSyncPullsMeetingFromOtherDevice(t *testing.T) {
	f := newSyncFixture(t)
	m := createMeeting(t, f.storeA, "Remote meeting")
	localDoc := readMeetingFile(t, f.storeA, m.ID, docFile)
	localTranscript := readMeetingFile(t, f.storeA, m.ID, transcriptFile)

	if _, err := f.syncA(); err != nil {
		t.Fatal(err)
	}
	result, err := f.syncB()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Pulled) != 1 || result.Pulled[0] != m.ID {
		t.Fatalf("pulled = %v, want [%s]", result.Pulled, m.ID)
	}

	// The meeting now exists on device B, byte-identical, and is
	// queryable through the store.
	got, err := f.storeB.Get(m.ID)
	if err != nil {
		t.Fatalf("pulled meeting not loadable via store: %v", err)
	}
	if got.ID != m.ID || got.Title != "Remote meeting" {
		t.Fatalf("pulled meeting = %+v", got)
	}
	if b := readMeetingFile(t, f.storeB, m.ID, docFile); b != localDoc {
		t.Fatalf("doc differs after pull:\n%s\n%s", localDoc, b)
	}
	if b := readMeetingFile(t, f.storeB, m.ID, transcriptFile); b != localTranscript {
		t.Fatalf("transcript differs after pull:\n%s\n%s", localTranscript, b)
	}
}

func TestSyncDetectsConflictAndPreservesBothVersions(t *testing.T) {
	f := newSyncFixture(t)
	m := createMeeting(t, f.storeA, "Contested")
	if _, err := f.syncA(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.syncB(); err != nil {
		t.Fatal(err)
	}

	// Both devices diverge without syncing in between.
	if err := f.storeA.UpdateDoc(m.ID, func(mm *meeting.Meeting) error {
		mm.Title = "Version A"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.storeB.UpdateDoc(m.ID, func(mm *meeting.Meeting) error {
		mm.Title = "Version B"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.syncA(); err != nil {
		t.Fatal(err)
	}
	resultB, err := f.syncB()
	if err != nil {
		t.Fatal(err)
	}
	if len(resultB.Conflicted) != 1 || resultB.Conflicted[0] != m.ID {
		t.Fatalf("conflicted = %v, want [%s]", resultB.Conflicted, m.ID)
	}

	// Loser (B) ends up with the winner's (A's) title live…
	got, err := f.storeB.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Version A" {
		t.Fatalf("live title on loser = %q, want Version A", got.Title)
	}

	// …and B's own divergent content preserved as conflict copies.
	dirB := filepath.Join(f.storeB.Root(), m.ID)
	entries, err := os.ReadDir(dirB)
	if err != nil {
		t.Fatal(err)
	}
	var conflictFiles []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "conflict-") {
			conflictFiles = append(conflictFiles, e.Name())
		}
	}
	if len(conflictFiles) < 2 { // .md and .jsonl at minimum
		t.Fatalf("conflict copies = %v, want at least meeting.md + utterances.jsonl copies", conflictFiles)
	}
	foundB := false
	for _, name := range conflictFiles {
		if strings.HasSuffix(name, ".md") {
			raw, err := os.ReadFile(filepath.Join(dirB, name))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "Version B") {
				foundB = true
			}
		}
	}
	if !foundB {
		t.Fatalf("no conflict copy contains Version B's content; copies: %v", conflictFiles)
	}

	// Winner's content is what's live remotely.
	ciphertext, _, err := f.client.GetBlob(f.token, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	files, err := DecryptMeetingBundle(f.masterKey, m.ID, ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files[docFile]), "Version A") {
		t.Fatalf("remote blob does not hold Version A:\n%s", files[docFile])
	}

	// Post-conflict sync on B is a no-op (B adopted remote as-is).
	resultB2, err := f.syncB()
	if err != nil {
		t.Fatal(err)
	}
	if len(resultB2.Pushed) != 0 || len(resultB2.Conflicted) != 0 {
		t.Fatalf("post-adoption sync not quiet: %+v", resultB2)
	}
}

func TestSyncPullsThenPushesRoundTrip(t *testing.T) {
	f := newSyncFixture(t)
	m := createMeeting(t, f.storeA, "Round trip")
	if _, err := f.syncA(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.syncB(); err != nil {
		t.Fatal(err)
	}

	// B edits and pushes; A pulls and sees the edit.
	if err := f.storeB.UpdateDoc(m.ID, func(mm *meeting.Meeting) error {
		mm.Summary = "edited on B"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.syncB(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.syncA(); err != nil {
		t.Fatal(err)
	}
	got, err := f.storeA.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "edited on B" {
		t.Fatalf("A's summary = %q, want B's edit", got.Summary)
	}
}

// newStoreAt builds a meeting.Store under its own directory inside
// home, so the store root never collides with the manifest file.
func newStoreAt(t *testing.T, home string) *meeting.Store {
	t.Helper()
	store, err := meeting.NewStore(filepath.Join(home, "meetings"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestAdoptRemoteRejectsInvalidMeetingID(t *testing.T) {
	f := newSyncFixture(t)
	err := adoptRemote(f.storeA, f.client, f.token, f.masterKey, "../../etc", syncState{}, &SyncResult{})
	if err == nil || !strings.Contains(err.Error(), "invalid meeting id") {
		t.Fatalf("err = %v, want invalid-meeting-id rejection", err)
	}
}
