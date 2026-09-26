package cloudsync

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/spacingmind/catchm/internal/e2ee"
)

// Bundle wire format (internal to cloudsync; only EncryptMeetingBundle
// and DecryptMeetingBundle read it): the plaintext blob passed to
// e2ee.WrapKey is a sequence of entries,
//
//	[4-byte big-endian filename length][filename bytes]
//	[4-byte big-endian content length][content bytes]
//
// repeated once per file, in the order the files map is marshaled.
// Files with the same name cannot occur — the input is a map.
const lengthPrefix = 4

// EncryptMeetingBundle encrypts a meeting's files under a key derived
// from the master key and the meeting ID, so no two meetings share
// ciphertext key material and the raw master key never encrypts data
// directly. The checksum is the sha256 hex of the ciphertext — what
// PUT /blobs/:id's X-Checksum header expects. The "catchm-cloud/..."
// HKDF info string is a frozen protocol identifier (predates the
// catchm-cloud -> catchm-api repo rename) and must stay byte-for-byte.
func EncryptMeetingBundle(masterKey []byte, meetingID string, files map[string][]byte) (ciphertext []byte, checksum string, err error) {
	plaintext := marshalBundle(files)
	meetingKey := e2ee.DeriveSubKey(masterKey, "catchm-cloud/meeting/"+meetingID)
	ciphertext, err = e2ee.WrapKey(plaintext, meetingKey)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(ciphertext)
	return ciphertext, hex.EncodeToString(sum[:]), nil
}

// DecryptMeetingBundle inverts EncryptMeetingBundle. A wrong master
// key fails inside UnwrapKey's AEAD check rather than producing
// garbage.
func DecryptMeetingBundle(masterKey []byte, meetingID string, ciphertext []byte) (map[string][]byte, error) {
	meetingKey := e2ee.DeriveSubKey(masterKey, "catchm-cloud/meeting/"+meetingID)
	plaintext, err := e2ee.UnwrapKey(ciphertext, meetingKey)
	if err != nil {
		return nil, err
	}
	return unmarshalBundle(plaintext)
}

func marshalBundle(files map[string][]byte) []byte {
	var buf []byte
	var lenBuf [lengthPrefix]byte
	for _, name := range sortedFileNames(files) {
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(name)))
		buf = append(buf, lenBuf[:]...)
		buf = append(buf, name...)

		content := files[name]
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(content)))
		buf = append(buf, lenBuf[:]...)
		buf = append(buf, content...)
	}
	return buf
}

func unmarshalBundle(plaintext []byte) (map[string][]byte, error) {
	files := map[string][]byte{}
	for off := 0; off < len(plaintext); {
		name, off2, err := readField(plaintext, off)
		if err != nil {
			return nil, err
		}
		content, off3, err := readField(plaintext, off2)
		if err != nil {
			return nil, err
		}
		if _, dup := files[string(name)]; dup {
			return nil, fmt.Errorf("cloudsync: duplicate file %q in bundle", name)
		}
		files[string(name)] = content
		off = off3
	}
	return files, nil
}

func readField(buf []byte, off int) (field []byte, next int, err error) {
	if off+lengthPrefix > len(buf) {
		return nil, 0, errTruncated
	}
	n := binary.BigEndian.Uint32(buf[off : off+lengthPrefix])
	start := off + lengthPrefix
	if uint64(start)+uint64(n) > uint64(len(buf)) {
		return nil, 0, errTruncated
	}
	return buf[start : start+int(n)], start + int(n), nil
}

var errTruncated = errors.New("cloudsync: truncated bundle")

// sortedFileNames gives bundles a deterministic field order so the
// same files always encrypt to a stable plaintext before the
// per-meeting key seals it (checksums and dedup depend on this).
func sortedFileNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}
