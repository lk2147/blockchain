package main

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateExternalKeyAndValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "external.hex")
	pub, key, err := createExternalKey(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 64 || string(pub[:]) != string(key[32:]) {
		t.Fatal("generated public/private key pair does not match")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := hex.DecodeString(string(b))
	if err != nil || string(decoded) != string(key) {
		t.Fatal("private key fixture was not stored correctly")
	}
	if _, _, err = createExternalKey(path, nil); err == nil {
		t.Fatal("existing external key file was overwritten")
	}
	if got := loadValue(12, 64); len(got) != 64 {
		t.Fatalf("value length = %d, want 64", len(got))
	}
}
