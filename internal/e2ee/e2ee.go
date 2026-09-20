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
	"strings"

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

// DeriveSubKey expands a secret into an independent 32-byte sub-key
// bound to the given context string. Callers compose their own info
// (e.g. "catchm-cloud/meeting/<id>" for per-meeting keys).
func DeriveSubKey(secret []byte, info string) []byte {
	return deriveHKDF(secret, info)
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

// recoveryAlphabet is a 32-character unambiguous alphabet: uppercase
// letters and digits with I, O, 0, and 1 removed to reduce
// transcription errors. 32 symbols means one random byte maps onto one
// character with zero modulo bias.
const recoveryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// GenerateRecoveryCode returns a high-entropy, human-transcribable
// recovery code: five groups of five characters separated by dashes
// (e.g. "ABCDE-FGHIJ-..."), 25 chars drawn from a 32-symbol alphabet ≈
// 125 bits of entropy.
//
// Decision (per spec): a BIP-39-style word-list mnemonic would need a
// ~2048-word list vendored into the repo or a new dependency; rather
// than hand-roll a small custom wordlist (weak, unaudited) or pull in a
// dependency not vetted for this milestone, the formatted random string
// was chosen — self-contained, exactly transcribable, and more entropy
// than the architecture doc's 24-random-char baseline.
func GenerateRecoveryCode() (string, error) {
	groups := make([]string, 5)
	buf := make([]byte, 5)
	for g := range groups {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("e2ee: recovery code entropy: %w", err)
		}
		chars := make([]byte, len(buf))
		for i, b := range buf {
			chars[i] = recoveryAlphabet[int(b)%len(recoveryAlphabet)]
		}
		groups[g] = string(chars)
	}
	return strings.Join(groups, "-"), nil
}

// DeriveRecoveryKey turns a recovery code into a key-wrapping key via
// the same Argon2id treatment as the password path (recovery codes are
// already high-entropy, but consistency with DeriveAuthKey is simpler
// to reason about and test than a special case). The params used at
// signup are stored server-side and must be passed back here so
// recovery stays reproducible even if DefaultKDFParams changes in a
// future code version.
func DeriveRecoveryKey(recoveryCode string, salt []byte, params KDFParams) []byte {
	return DeriveAuthKey(recoveryCode, salt, params)
}

// DeriveRecoveryAuthHash derives the value the Worker compares during
// POST /recover, mirroring DeriveAuthHash's relationship to authKey.
// It is never used to decrypt anything, so a server-side leak of it
// does not help unwrap the recovery blob.
func DeriveRecoveryAuthHash(recoveryKey []byte) []byte {
	return deriveHKDF(recoveryKey, "catchm-cloud/recovery-auth-hash")
}

// DeriveRecoveryWrapKey derives the sub-key that actually wraps the
// recovery blob. It never leaves the client, mirroring
// DeriveKeyWrapKey.
func DeriveRecoveryWrapKey(recoveryKey []byte) []byte {
	return deriveHKDF(recoveryKey, "catchm-cloud/recovery-wrap")
}

// saltLen is the length of the random salts generated at signup
// (auth_salt and recovery_salt). 16 bytes is comfortably above the
// 8-byte minimum for Argon2id salts.
const saltLen = 16

func randomSalt() ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("e2ee: salt: %w", err)
	}
	return salt, nil
}

// AccountSecrets bundles everything the signup flow produces: what
// gets sent to the Worker, what stays local, and the recovery values.
// MasterKey and RecoveryCode never leave the client; RecoveryCode is
// shown to the user exactly once.
type AccountSecrets struct {
	MasterKey         []byte
	AuthSalt          []byte
	AuthHash          []byte
	KDFParams         KDFParams
	WrappedMasterKey  []byte
	RecoveryCode      string
	RecoverySalt      []byte
	RecoveryAuthHash  []byte
	RecoveryBlob      []byte
	RecoveryKDFParams KDFParams
}

// SetupAccount performs the full first-device signup derivation:
// master key, password-derived wrapping, and the recovery blob. There
// is no separate KeyWrapParams — the key_wrap_key is re-derived from
// the same Argon2id run (KDFParams) that produced auth_hash, so
// UnlockWithPassword only needs AuthSalt + KDFParams.
func SetupAccount(password string) (*AccountSecrets, error) {
	masterKey := GenerateMasterKey()
	pw, err := RewrapForPassword(masterKey, password)
	if err != nil {
		return nil, err
	}
	rw, err := RewrapForRecovery(masterKey)
	if err != nil {
		return nil, err
	}
	return &AccountSecrets{
		MasterKey:         masterKey,
		AuthSalt:          pw.AuthSalt,
		AuthHash:          pw.AuthHash,
		KDFParams:         pw.KDFParams,
		WrappedMasterKey:  pw.WrappedMasterKey,
		RecoveryCode:      rw.RecoveryCode,
		RecoverySalt:      rw.RecoverySalt,
		RecoveryAuthHash:  rw.RecoveryAuthHash,
		RecoveryBlob:      rw.RecoveryBlob,
		RecoveryKDFParams: rw.RecoveryKDFParams,
	}, nil
}

// PasswordWrap is the password-derived half of an account: everything
// a signup or reset-password call needs to store for the password
// path. Produced by RewrapForPassword.
type PasswordWrap struct {
	AuthSalt         []byte
	AuthHash         []byte
	KDFParams        KDFParams
	WrappedMasterKey []byte
}

// RewrapForPassword wraps masterKey under a fresh salt + the given
// password's key_wrap_key, returning all values reset-password must
// store. SetupAccount calls this; password reset calls it again with
// the recovered master key (it already has a master key — no new one
// is generated).
func RewrapForPassword(masterKey []byte, password string) (*PasswordWrap, error) {
	authSalt, err := randomSalt()
	if err != nil {
		return nil, err
	}
	params := DefaultKDFParams
	authKey := DeriveAuthKey(password, authSalt, params)
	wrappedMasterKey, err := WrapKey(masterKey, DeriveKeyWrapKey(authKey))
	if err != nil {
		return nil, err
	}
	return &PasswordWrap{
		AuthSalt:         authSalt,
		AuthHash:         DeriveAuthHash(authKey),
		KDFParams:        params,
		WrappedMasterKey: wrappedMasterKey,
	}, nil
}

// RecoveryWrap is the recovery-code-derived half of an account: a
// fresh code, its salt, the wrapped master key, and the
// server-verifiable recovery auth hash. Produced by RewrapForRecovery.
type RecoveryWrap struct {
	RecoveryCode      string
	RecoverySalt      []byte
	RecoveryAuthHash  []byte
	RecoveryBlob      []byte
	RecoveryKDFParams KDFParams
}

// RewrapForRecovery generates a FRESH recovery code and wraps
// masterKey under its derived wrap key. Reset-password rotates the
// recovery code (after a real recovery the old code must stop working,
// same reasoning as rotating a backup 2FA code after use), so this
// never reuses inputs from a previous wrap.
func RewrapForRecovery(masterKey []byte) (*RecoveryWrap, error) {
	recoveryCode, err := GenerateRecoveryCode()
	if err != nil {
		return nil, err
	}
	recoverySalt, err := randomSalt()
	if err != nil {
		return nil, err
	}
	params := DefaultKDFParams
	recoveryKey := DeriveRecoveryKey(recoveryCode, recoverySalt, params)
	recoveryBlob, err := WrapKey(masterKey, DeriveRecoveryWrapKey(recoveryKey))
	if err != nil {
		return nil, err
	}
	return &RecoveryWrap{
		RecoveryCode:      recoveryCode,
		RecoverySalt:      recoverySalt,
		RecoveryAuthHash:  DeriveRecoveryAuthHash(recoveryKey),
		RecoveryBlob:      recoveryBlob,
		RecoveryKDFParams: params,
	}, nil
}

// UnlockWithPassword re-derives the key_wrap_key from the password and
// the salt/params a login response carries, and unwraps the master
// key. A wrong password yields a wrong key_wrap_key, which the AEAD
// open rejects.
func UnlockWithPassword(password string, salt []byte, params KDFParams, wrappedMasterKey []byte) ([]byte, error) {
	authKey := DeriveAuthKey(password, salt, params)
	return UnwrapKey(wrappedMasterKey, DeriveKeyWrapKey(authKey))
}

// RecoverMasterKey unwraps the master key from the recovery blob using
// the recovery code and the salt/params stored alongside it.
func RecoverMasterKey(recoveryCode string, salt []byte, params KDFParams, recoveryBlob []byte) ([]byte, error) {
	recoveryKey := DeriveRecoveryKey(recoveryCode, salt, params)
	return UnwrapKey(recoveryBlob, DeriveRecoveryWrapKey(recoveryKey))
}
