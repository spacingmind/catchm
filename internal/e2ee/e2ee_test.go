package e2ee

import (
	"bytes"
	"testing"
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
