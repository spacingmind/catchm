package e2ee

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

func TestGenerateMasterKeyReturns32Bytes(t *testing.T) {
	key := GenerateMasterKey()
	if len(key) != 32 {
		t.Fatalf("key length = %d, want 32", len(key))
	}
}

func TestGenerateMasterKeyIsRandom(t *testing.T) {
	a := GenerateMasterKey()
	b := GenerateMasterKey()
	if bytes.Equal(a, b) {
		t.Fatal("two GenerateMasterKey calls returned the same value")
	}
}

func TestSameInputsDeterministic(t *testing.T) {
	salt := []byte("some-salt-0123456789")
	password := "correct horse battery staple"

	key1 := DeriveAuthKey(password, salt, DefaultKDFParams)
	key2 := DeriveAuthKey(password, salt, DefaultKDFParams)
	if !bytes.Equal(key1, key2) {
		t.Fatal("DeriveAuthKey is not deterministic for identical inputs")
	}
	if !bytes.Equal(DeriveAuthHash(key1), DeriveAuthHash(key2)) {
		t.Fatal("DeriveAuthHash is not deterministic")
	}
	if !bytes.Equal(DeriveKeyWrapKey(key1), DeriveKeyWrapKey(key2)) {
		t.Fatal("DeriveKeyWrapKey is not deterministic")
	}
}

func TestDifferentSaltsProduceDifferentKeys(t *testing.T) {
	password := "correct horse battery staple"
	key1 := DeriveAuthKey(password, []byte("salt-aaaaaaaaaaaaaa"), DefaultKDFParams)
	key2 := DeriveAuthKey(password, []byte("salt-bbbbbbbbbbbbbb"), DefaultKDFParams)
	if bytes.Equal(key1, key2) {
		t.Fatal("different salts produced the same authKey")
	}
}

func TestDeriveAuthHashAndKeyWrapKeyAreIndependent(t *testing.T) {
	authKey := GenerateMasterKey()
	authHash := DeriveAuthHash(authKey)
	keyWrapKey := DeriveKeyWrapKey(authKey)
	if len(authHash) != 32 || len(keyWrapKey) != 32 {
		t.Fatalf("lengths = %d/%d, want 32/32", len(authHash), len(keyWrapKey))
	}
	if len(authHash) == 0 || len(keyWrapKey) == 0 {
		t.Fatal("derived values must be non-empty")
	}
	if bytes.Equal(authHash, keyWrapKey) {
		t.Fatal("auth_hash and key_wrap_key derived from same authKey are identical")
	}
}

func TestWrapUnwrapRoundTrip(t *testing.T) {
	masterKey := GenerateMasterKey()
	wrappingKey := GenerateMasterKey()

	wrapped, err := WrapKey(masterKey, wrappingKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(wrapped) != chacha20poly1305.NonceSizeX+len(masterKey)+chacha20poly1305.Overhead {
		t.Fatalf("wrapped length = %d, want nonce+ciphertext+tag", len(wrapped))
	}

	got, err := UnwrapKey(wrapped, wrappingKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, masterKey) {
		t.Fatal("unwrapped key does not match original")
	}
}

func TestUnwrapFailsWithWrongKey(t *testing.T) {
	masterKey := GenerateMasterKey()
	wrapped, err := WrapKey(masterKey, GenerateMasterKey())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnwrapKey(wrapped, GenerateMasterKey()); err == nil {
		t.Fatal("unwrap with wrong wrappingKey unexpectedly succeeded")
	}
}

func TestUnwrapFailsOnTruncatedInput(t *testing.T) {
	wrappingKey := GenerateMasterKey()
	wrapped, err := WrapKey(GenerateMasterKey(), wrappingKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{0, 1, 12, chacha20poly1305.NonceSizeX - 1} {
		if _, err := UnwrapKey(wrapped[:n], wrappingKey); err == nil {
			t.Fatalf("unwrap of %d-byte truncated input unexpectedly succeeded", n)
		}
	}
}

func TestUnwrapFailsOnTamperedCiphertext(t *testing.T) {
	wrappingKey := GenerateMasterKey()
	masterKey := GenerateMasterKey()
	wrapped, err := WrapKey(masterKey, wrappingKey)
	if err != nil {
		t.Fatal(err)
	}

	for _, i := range []int{
		chacha20poly1305.NonceSizeX, // first ciphertext byte
		len(wrapped) / 2,            // middle
		len(wrapped) - 1,            // last tag byte
		chacha20poly1305.NonceSizeX + chacha20poly1305.Overhead/2, // inside ciphertext
	} {
		tampered := bytes.Clone(wrapped)
		tampered[i] ^= 0x01
		if _, err := UnwrapKey(tampered, wrappingKey); err == nil {
			t.Fatalf("unwrap of input tampered at byte %d unexpectedly succeeded", i)
		}
	}
}

func TestGenerateRecoveryCodeFormat(t *testing.T) {
	code, err := GenerateRecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 29 { // 5 groups of 5 chars + 4 dashes
		t.Fatalf("code length = %d, want 29", len(code))
	}
	groups := strings.Split(code, "-")
	if len(groups) != 5 {
		t.Fatalf("code has %d groups, want 5", len(groups))
	}
	for _, g := range groups {
		if len(g) != 5 {
			t.Fatalf("group %q has length %d, want 5", g, len(g))
		}
		for _, c := range g {
			if !strings.ContainsRune(recoveryAlphabet, c) {
				t.Fatalf("group %q contains character %q outside the alphabet", g, c)
			}
		}
	}
}

func TestGenerateRecoveryCodeIsRandom(t *testing.T) {
	a, err := GenerateRecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateRecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two GenerateRecoveryCode calls returned the same code")
	}
}

func TestDeriveRecoveryKeyDeterministic(t *testing.T) {
	salt := []byte("recovery-salt-123")
	key1 := DeriveRecoveryKey("ABCDE-FGHIJ-KLMNP-QRSTU-VWXYZ", salt, DefaultKDFParams)
	key2 := DeriveRecoveryKey("ABCDE-FGHIJ-KLMNP-QRSTU-VWXYZ", salt, DefaultKDFParams)
	if !bytes.Equal(key1, key2) {
		t.Fatal("DeriveRecoveryKey is not deterministic for identical inputs")
	}
}

func TestDeriveRecoveryKeyDifferentCodes(t *testing.T) {
	salt := []byte("recovery-salt-123")
	key1 := DeriveRecoveryKey("ABCDE-FGHIJ-KLMNP-QRSTU-VWXYZ", salt, DefaultKDFParams)
	key2 := DeriveRecoveryKey("VWXYZ-KLMNP-FGHIJ-ABCDE-QRSTU", salt, DefaultKDFParams)
	if bytes.Equal(key1, key2) {
		t.Fatal("different recovery codes produced the same key")
	}
}

func TestSetupAccountThenUnlockRoundTrips(t *testing.T) {
	secrets, err := SetupAccount("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(DeriveAuthHash(DeriveAuthKey("correct horse battery staple", secrets.AuthSalt, secrets.KDFParams)), secrets.AuthHash) {
		t.Fatal("AuthHash does not match an independent re-derivation")
	}

	masterKey, err := UnlockWithPassword("correct horse battery staple", secrets.AuthSalt, secrets.KDFParams, secrets.WrappedMasterKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(masterKey, secrets.MasterKey) {
		t.Fatal("unlock recovered a different master key than signup generated")
	}
}

func TestUnlockFailsWithWrongPassword(t *testing.T) {
	secrets, err := SetupAccount("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	masterKey, err := UnlockWithPassword("wrong horse battery staple", secrets.AuthSalt, secrets.KDFParams, secrets.WrappedMasterKey)
	if err == nil {
		t.Fatal("unlock with wrong password unexpectedly succeeded")
	}
	if masterKey != nil {
		t.Fatal("failed unlock must not return key bytes")
	}
}

func TestRecoveryRoundTrips(t *testing.T) {
	secrets, err := SetupAccount("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	masterKey, err := RecoverMasterKey(secrets.RecoveryCode, secrets.RecoverySalt, secrets.RecoveryKDFParams, secrets.RecoveryBlob)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(masterKey, secrets.MasterKey) {
		t.Fatal("recovery recovered a different master key than signup generated")
	}
}

func TestRecoveryFailsWithWrongCode(t *testing.T) {
	secrets, err := SetupAccount("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	masterKey, err := RecoverMasterKey("VWXYZ-KLMNP-FGHIJ-ABCDE-QRSTU", secrets.RecoverySalt, secrets.RecoveryKDFParams, secrets.RecoveryBlob)
	if err == nil {
		t.Fatal("recovery with wrong code unexpectedly succeeded")
	}
	if masterKey != nil {
		t.Fatal("failed recovery must not return key bytes")
	}
}

// Regression test for the bug where RecoverMasterKey accepted a params
// argument but DeriveRecoveryKey hardcoded DefaultKDFParams: recovery
// must honor exactly the KDFParams the blob was wrapped with, even
// when they differ from DefaultKDFParams.
func TestRecoverMasterKeyHonorsNonDefaultParams(t *testing.T) {
	custom := KDFParams{Time: 1, Memory: 64, Parallelism: 1, KeyLen: 32}
	if custom == DefaultKDFParams {
		t.Fatal("custom params must differ from DefaultKDFParams for this test to mean anything")
	}

	code, err := GenerateRecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	salt := []byte("recovery-salt-123")
	masterKey := GenerateMasterKey()
	blob, err := WrapKey(masterKey, DeriveRecoveryWrapKey(DeriveRecoveryKey(code, salt, custom)))
	if err != nil {
		t.Fatal(err)
	}

	got, err := RecoverMasterKey(code, salt, custom, blob)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, masterKey) {
		t.Fatal("recovery with matching non-default params failed to recover the master key")
	}

	// With params genuinely honored, mismatched params must fail the
	// AEAD open rather than silently re-deriving with defaults.
	if _, err := RecoverMasterKey(code, salt, DefaultKDFParams, blob); err == nil {
		t.Fatal("recovery with mismatched params unexpectedly succeeded — params are not being honored")
	}
}

// The "catchm-cloud/..." literals below mirror e2ee.go's frozen HKDF
// info strings and must stay byte-for-byte in sync with them.
func TestDeriveSubKey(t *testing.T) {
	secret := GenerateMasterKey()
	a := DeriveSubKey(secret, "catchm-cloud/meeting/meet-1")
	b := DeriveSubKey(secret, "catchm-cloud/meeting/meet-2")
	if len(a) != 32 {
		t.Fatalf("sub-key length = %d, want 32", len(a))
	}
	if bytes.Equal(a, b) {
		t.Fatal("different info strings produced the same sub-key")
	}
	if bytes.Equal(a, secret) {
		t.Fatal("sub-key equals the input secret")
	}
	if !bytes.Equal(a, DeriveSubKey(secret, "catchm-cloud/meeting/meet-1")) {
		t.Fatal("DeriveSubKey is not deterministic")
	}
	if !bytes.Equal(DeriveSubKey(secret, "catchm-cloud/key-wrap"), DeriveKeyWrapKey(secret)) {
		t.Fatal("DeriveSubKey disagrees with the fixed-info derivation it generalizes")
	}
}

func TestRecoverySplitKeysAreIndependent(t *testing.T) {
	recoveryKey := GenerateMasterKey()
	authHash := DeriveRecoveryAuthHash(recoveryKey)
	wrapKey := DeriveRecoveryWrapKey(recoveryKey)
	if len(authHash) != 32 || len(wrapKey) != 32 {
		t.Fatalf("lengths = %d/%d, want 32/32", len(authHash), len(wrapKey))
	}
	if bytes.Equal(authHash, wrapKey) {
		t.Fatal("recovery_auth_hash and recovery wrap key are identical — split is cosmetic")
	}
	if bytes.Equal(authHash, recoveryKey) || bytes.Equal(wrapKey, recoveryKey) {
		t.Fatal("sub-key equals raw recoveryKey")
	}
}

// Regression proof the split changed real behavior: a blob wrapped
// with the raw recoveryKey (M13b's old shape) must NOT unwrap with
// DeriveRecoveryWrapKey, and vice versa. If this passes trivially,
// the wrap-key change never took effect.
func TestRawRecoveryKeyWrapIsRejected(t *testing.T) {
	recoveryCode := "ABCDE-FGHIJ-KLMNP-QRSTU-VWXYZ"
	salt := []byte("recovery-salt-123")
	recoveryKey := DeriveRecoveryKey(recoveryCode, salt, DefaultKDFParams)
	masterKey := GenerateMasterKey()

	oldShapeBlob, err := WrapKey(masterKey, recoveryKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnwrapKey(oldShapeBlob, DeriveRecoveryWrapKey(recoveryKey)); err == nil {
		t.Fatal("blob wrapped with raw recoveryKey unwrapped with DeriveRecoveryWrapKey — split is not real")
	}

	newShapeBlob, err := WrapKey(masterKey, DeriveRecoveryWrapKey(recoveryKey))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnwrapKey(newShapeBlob, recoveryKey); err == nil {
		t.Fatal("blob wrapped with DeriveRecoveryWrapKey unwrapped with raw recoveryKey")
	}
}

func TestSetupAccountNewShapeRoundTrips(t *testing.T) {
	secrets, err := SetupAccount("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if len(secrets.RecoveryAuthHash) != 32 {
		t.Fatalf("RecoveryAuthHash length = %d, want 32", len(secrets.RecoveryAuthHash))
	}

	// Recovery path through the new shape.
	masterKey, err := RecoverMasterKey(secrets.RecoveryCode, secrets.RecoverySalt, secrets.RecoveryKDFParams, secrets.RecoveryBlob)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(masterKey, secrets.MasterKey) {
		t.Fatal("recovery recovered a different master key than signup generated")
	}

	// RecoveryAuthHash is derivable from the code — what the server
	// will compare during POST /recover.
	recoveryKey := DeriveRecoveryKey(secrets.RecoveryCode, secrets.RecoverySalt, secrets.RecoveryKDFParams)
	if !bytes.Equal(DeriveRecoveryAuthHash(recoveryKey), secrets.RecoveryAuthHash) {
		t.Fatal("stored RecoveryAuthHash does not match an independent derivation")
	}

	// Password path still round-trips.
	got, err := UnlockWithPassword("correct horse battery staple", secrets.AuthSalt, secrets.KDFParams, secrets.WrappedMasterKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, secrets.MasterKey) {
		t.Fatal("unlock recovered a different master key")
	}
}

// RewrapForPassword/RewrapForRecovery are what password reset calls
// with an already-recovered master key: the same master key must
// unwrap under the new password, and the rotated recovery code must
// supersede the old one.
func TestRewrapRotateFlow(t *testing.T) {
	secrets, err := SetupAccount("old password")
	if err != nil {
		t.Fatal(err)
	}
	masterKey, err := RecoverMasterKey(secrets.RecoveryCode, secrets.RecoverySalt, secrets.RecoveryKDFParams, secrets.RecoveryBlob)
	if err != nil {
		t.Fatal(err)
	}

	pw, err := RewrapForPassword(masterKey, "new password")
	if err != nil {
		t.Fatal(err)
	}
	rw, err := RewrapForRecovery(masterKey)
	if err != nil {
		t.Fatal(err)
	}

	// New password unlocks the SAME master key.
	got, err := UnlockWithPassword("new password", pw.AuthSalt, pw.KDFParams, pw.WrappedMasterKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, masterKey) {
		t.Fatal("rewrapped master key differs — data would be lost")
	}
	// Old password no longer unlocks it.
	if _, err := UnlockWithPassword("old password", pw.AuthSalt, pw.KDFParams, pw.WrappedMasterKey); err == nil {
		t.Fatal("old password still unlocks after rewrap")
	}

	// New recovery code recovers the same master key…
	got, err = RecoverMasterKey(rw.RecoveryCode, rw.RecoverySalt, rw.RecoveryKDFParams, rw.RecoveryBlob)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, masterKey) {
		t.Fatal("rotated recovery blob holds a different master key")
	}
	// …and the old code no longer does.
	if _, err := RecoverMasterKey(secrets.RecoveryCode, secrets.RecoverySalt, secrets.RecoveryKDFParams, rw.RecoveryBlob); err == nil {
		t.Fatal("old recovery code still works against the rotated blob")
	}
	if rw.RecoveryCode == secrets.RecoveryCode {
		t.Fatal("recovery code was not rotated")
	}

	// Rewrap outputs are independently derived from each other and
	// from the old signup values.
	if bytes.Equal(rw.RecoveryAuthHash, secrets.RecoveryAuthHash) {
		t.Fatal("rotated recovery auth hash identical to the old one")
	}
	if bytes.Equal(pw.AuthSalt, secrets.AuthSalt) {
		t.Fatal("rewrap reused the old auth salt")
	}
}
