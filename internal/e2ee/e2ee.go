// Package e2ee implements the client-side cryptography for cloud sync:
// key generation, Argon2id key derivation with HKDF-separated auth and
// key-wrapping secrets, XChaCha20-Poly1305 key wrapping, and
// recovery-code handling. See docs/cloud-sync-architecture.md for the
// composition these primitives build into.
package e2ee

import "crypto/rand"

// masterKeyLen is the length of the random master key in bytes. It is
// the XChaCha20-Poly1305 key size (32 bytes) and doubles as the key
// length for every derived secret in this package.
const masterKeyLen = 32

// GenerateMasterKey returns 32 random bytes from crypto/rand. The
// master key is generated once on the first device, never sent to the
// server in the clear, and is what actually encrypts meeting data.
func GenerateMasterKey() []byte {
	key := make([]byte, masterKeyLen)
	if _, err := rand.Read(key); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return key
}

// KDFParams holds the Argon2id parameters used to derive a key. It is
// JSON-marshaled into the opaque kdf_params string the cloud Worker
// stores, so a login can re-run the exact derivation the signup used.
type KDFParams struct {
	Time        uint32 `json:"time"`
	Memory      uint32 `json:"memory"`
	Parallelism uint8  `json:"parallelism"`
	KeyLen      uint32 `json:"key_len"`
}

// DefaultKDFParams follows the OWASP Password Storage Cheat Sheet's
// current Argon2id recommendation (minimum configuration, m=19 MiB,
// t=2, p=1; https://cheatsheetseries.owasp.org — Argon2id section).
// KeyLen matches the 32-byte key size used throughout this package.
var DefaultKDFParams = KDFParams{
	Time:        2,
	Memory:      19456,
	Parallelism: 1,
	KeyLen:      masterKeyLen,
}
