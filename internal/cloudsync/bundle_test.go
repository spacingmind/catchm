package cloudsync

import (
	"bytes"
	"testing"

	"github.com/spacingmind/catchm/internal/e2ee"
)

func testFiles(withV2 bool) map[string][]byte {
	files := map[string][]byte{
		"meeting.md":       []byte("# Meeting\n\nNotes with unicode: bạn ổn không?\n"),
		"utterances.jsonl": []byte("{\"t\":0,\"text\":\"hello\"}\n{\"t\":5,\"text\":\"world\"}\n"),
	}
	if withV2 {
		files["utterances.v2.jsonl"] = []byte("{\"t\":0,\"speaker\":\"spk1\",\"text\":\"hello\"}\n")
	}
	return files
}

func TestEncryptDecryptMeetingBundleRoundTrips(t *testing.T) {
	masterKey := e2ee.GenerateMasterKey()
	files := testFiles(true)

	ciphertext, checksum, err := EncryptMeetingBundle(masterKey, "meet-1", files)
	if err != nil {
		t.Fatal(err)
	}
	if checksum == "" || len(ciphertext) == 0 {
		t.Fatalf("empty checksum/ciphertext: %q %d bytes", checksum, len(ciphertext))
	}
	for _, name := range []string{"meeting.md", "utterances.jsonl"} {
		if bytes.Contains(ciphertext, files[name]) {
			t.Fatalf("%s plaintext visible in ciphertext", name)
		}
	}

	got, err := DecryptMeetingBundle(masterKey, "meet-1", ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(files) {
		t.Fatalf("round trip lost files: %v", keysOf(got))
	}
	for name, content := range files {
		if !bytes.Equal(got[name], content) {
			t.Fatalf("file %q changed in round trip:\ngot  %q\nwant %q", name, got[name], content)
		}
	}
}

func TestBundleWithoutV2FileRoundTrips(t *testing.T) {
	masterKey := e2ee.GenerateMasterKey()
	files := testFiles(false)

	ciphertext, _, err := EncryptMeetingBundle(masterKey, "meet-2", files)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecryptMeetingBundle(masterKey, "meet-2", ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 files, got %v", keysOf(got))
	}
	if _, hasV2 := got["utterances.v2.jsonl"]; hasV2 {
		t.Fatal("v2 file materialized from nothing")
	}
}

func TestDecryptFailsWithWrongMasterKey(t *testing.T) {
	masterKey := e2ee.GenerateMasterKey()
	ciphertext, _, err := EncryptMeetingBundle(masterKey, "meet-1", testFiles(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptMeetingBundle(e2ee.GenerateMasterKey(), "meet-1", ciphertext); err == nil {
		t.Fatal("decrypt with wrong master key unexpectedly succeeded")
	}
}

func TestDecryptFailsWithWrongMeetingID(t *testing.T) {
	masterKey := e2ee.GenerateMasterKey()
	ciphertext, _, err := EncryptMeetingBundle(masterKey, "meet-1", testFiles(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptMeetingBundle(masterKey, "meet-other", ciphertext); err == nil {
		t.Fatal("decrypt with wrong meeting ID unexpectedly succeeded")
	}
}

func TestEncryptIsNonDeterministicAcrossNonce(t *testing.T) {
	masterKey := e2ee.GenerateMasterKey()
	a, _, err := EncryptMeetingBundle(masterKey, "meet-1", testFiles(false))
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := EncryptMeetingBundle(masterKey, "meet-1", testFiles(false))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("two encryptions of the same bundle are identical (nonce reuse?)")
	}
}

// The "catchm-cloud/meeting/..." literals below mirror bundle.go's
// frozen HKDF info string and must stay byte-for-byte in sync with it.
func TestUnmarshalRejectsTruncatedBundle(t *testing.T) {
	masterKey := e2ee.GenerateMasterKey()
	ciphertext, _, err := EncryptMeetingBundle(masterKey, "meet-1", testFiles(false))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := e2ee.UnwrapKey(ciphertext, e2ee.DeriveSubKey(masterKey, "catchm-cloud/meeting/meet-1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, cut := range []int{1, 3, 5, 10, len(plaintext) - 1} {
		if cut >= len(plaintext) {
			continue
		}
		// Re-seal the truncated plaintext so DecryptMeetingBundle's
		// framing layer (not the AEAD layer) is what rejects it.
		resealed, err := e2ee.WrapKey(plaintext[:cut], e2ee.DeriveSubKey(masterKey, "catchm-cloud/meeting/meet-1"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecryptMeetingBundle(masterKey, "meet-1", resealed); err == nil {
			t.Fatalf("truncated bundle at %d bytes unexpectedly parsed", cut)
		}
	}
}

func keysOf(m map[string][]byte) []string {
	names := sortedFileNames(m)
	return names
}
