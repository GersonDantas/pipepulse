package secrets

import (
	"bytes"
	"testing"
)

func TestBoxEncryptsAndAuthenticatesSecrets(t *testing.T) {
	box, err := NewBox([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewBox() error = %v", err)
	}

	ciphertext, err := box.Encrypt([]byte("repository-webhook-secret"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if bytes.Contains(ciphertext, []byte("repository-webhook-secret")) {
		t.Fatal("ciphertext contains plaintext secret")
	}
	plaintext, err := box.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if string(plaintext) != "repository-webhook-secret" {
		t.Fatalf("Decrypt() = %q, want original secret", plaintext)
	}

	ciphertext[len(ciphertext)-1] ^= 1
	if _, err := box.Decrypt(ciphertext); err == nil {
		t.Fatal("Decrypt(tampered) error = nil, want authentication failure")
	}
}

func TestNewBoxRequiresAES256Key(t *testing.T) {
	if _, err := NewBox([]byte("too-short")); err == nil {
		t.Fatal("NewBox() error = nil, want invalid key length")
	}
}
