// Package e2ee implements the client-side cryptography for cloud sync:
// key generation, Argon2id key derivation with HKDF-separated auth and
// key-wrapping secrets, XChaCha20-Poly1305 key wrapping, and
// recovery-code handling. See docs/cloud-sync-architecture.md for the
// composition these primitives build into.
package e2ee

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

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

// DeriveAuthKey runs Argon2id over the password with the given salt and
// parameters. The result is the account's root auth_key: it never
// leaves the client, and both auth_hash and key_wrap_key are derived
// from it.
func DeriveAuthKey(password string, salt []byte, params KDFParams) []byte {
	return argon2.IDKey([]byte(password), salt, params.Time, params.Memory, params.Parallelism, params.KeyLen)
}

// deriveHKDF expands a secret into keyLen bytes of key material with
// the given context string, via HKDF-SHA256.
func deriveHKDF(secret []byte, info string) []byte {
	r := hkdf.New(sha256.New, secret, nil, []byte(info))
	out := make([]byte, masterKeyLen)
	if _, err := io.ReadFull(r, out); err != nil {
		panic("hkdf: " + err.Error())
	}
	return out
}

// DeriveAuthHash derives the value sent to the Worker as auth_hash. It
// is a separate HKDF output from the key_wrap_key so that a leak of the
// server-stored auth_hash does not hand an attacker the key-wrapping
// secret.
func DeriveAuthHash(authKey []byte) []byte {
	return deriveHKDF(authKey, "catchm-cloud/auth-hash")
}

// DeriveKeyWrapKey derives the local secret that wraps the master key.
func DeriveKeyWrapKey(authKey []byte) []byte {
	return deriveHKDF(authKey, "catchm-cloud/key-wrap")
}

// WrapKey seals plaintextKey under wrappingKey with XChaCha20-Poly1305.
// The wire format is: 24-byte random nonce || AEAD ciphertext+tag
// (ciphertext includes the 16-byte Poly1305 tag). M13c depends on this
// exact byte layout — it is what gets stored server-side as
// wrapped_master_key and recovery_blob.
func WrapKey(plaintextKey, wrappingKey []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(wrappingKey)
	if err != nil {
		return nil, fmt.Errorf("e2ee: bad wrapping key: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("e2ee: nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, plaintextKey, nil), nil
}

// UnwrapKey opens a value produced by WrapKey. It returns an error —
// never panics — for a wrong wrappingKey, input shorter than the
// 24-byte nonce, or any tampering with the ciphertext or tag (AEAD
// authentication failure).
func UnwrapKey(wrapped, wrappingKey []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(wrappingKey)
	if err != nil {
		return nil, fmt.Errorf("e2ee: bad wrapping key: %w", err)
	}
	if len(wrapped) < aead.NonceSize() {
		return nil, fmt.Errorf("e2ee: wrapped value too short (%d bytes, minimum %d)", len(wrapped), aead.NonceSize())
	}
	nonce, ciphertext := wrapped[:aead.NonceSize()], wrapped[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("e2ee: unwrap failed: %w", err)
	}
	return plaintext, nil
}
