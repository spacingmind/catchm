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
