package store

import (
	"strings"
	"testing"
)

func TestIntegrationSecretEncryption(t *testing.T) {
	key := []byte("test-session-secret-with-at-least-32-bytes")
	store := &Store{encryptionSecret: key}
	secret := "smtp-password-that-must-not-be-stored-in-plain-text"

	encrypted, err := store.encryptSecret(secret)
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}
	if strings.Contains(encrypted, secret) {
		t.Fatal("encrypted setting contains its plaintext")
	}
	decrypted, err := store.decryptSecret(encrypted)
	if err != nil {
		t.Fatalf("decrypt secret: %v", err)
	}
	if decrypted != secret {
		t.Fatalf("decrypted secret = %q, want original value", decrypted)
	}

	otherStore := &Store{encryptionSecret: []byte("a-different-session-secret-of-the-same-size")}
	if _, err := otherStore.decryptSecret(encrypted); err == nil {
		t.Fatal("decrypting with a different key succeeded")
	}
}

func TestEmptyIntegrationSecretRemainsEmpty(t *testing.T) {
	store := &Store{encryptionSecret: []byte("test-session-secret-with-at-least-32-bytes")}
	encrypted, err := store.encryptSecret("")
	if err != nil {
		t.Fatalf("encrypt empty secret: %v", err)
	}
	if encrypted != "" {
		t.Fatalf("encrypted empty secret = %q, want empty value", encrypted)
	}
	decrypted, err := store.decryptSecret(encrypted)
	if err != nil || decrypted != "" {
		t.Fatalf("decrypt empty secret = %q, %v", decrypted, err)
	}
}
