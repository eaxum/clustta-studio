package cryptoutil

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	return key
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := newKey(t)
	cases := [][]byte{
		[]byte(""),
		[]byte("hello"),
		[]byte(`{"email":"svc@studio.com","password":"hunter2"}`),
	}
	for _, plaintext := range cases {
		ct, err := Encrypt(key, plaintext)
		if err != nil {
			t.Fatalf("Encrypt failed: %v", err)
		}
		got, err := Decrypt(key, ct)
		if err != nil {
			t.Fatalf("Decrypt failed: %v", err)
		}
		if string(got) != string(plaintext) {
			t.Fatalf("round-trip mismatch: want %q got %q", plaintext, got)
		}
	}
}

func TestDecryptWithWrongKeyFails(t *testing.T) {
	key1 := newKey(t)
	key2 := newKey(t)
	ct, err := Encrypt(key1, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	if _, err := Decrypt(key2, ct); err == nil {
		t.Fatal("expected error decrypting with wrong key, got nil")
	}
}

func TestDecryptTamperedCiphertextFails(t *testing.T) {
	key := newKey(t)
	ct, err := Encrypt(key, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(ct)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	raw[len(raw)-1] ^= 0x01
	tampered := base64.StdEncoding.EncodeToString(raw)
	if _, err := Decrypt(key, tampered); err == nil {
		t.Fatal("expected error decrypting tampered ciphertext, got nil")
	}
}

func TestEncryptUniqueNonces(t *testing.T) {
	key := newKey(t)
	a, _ := Encrypt(key, []byte("same"))
	b, _ := Encrypt(key, []byte("same"))
	if a == b {
		t.Fatal("encrypting the same plaintext twice produced identical ciphertext (nonce reuse)")
	}
}

func TestDecodeKeyInvalidLength(t *testing.T) {
	short := base64.StdEncoding.EncodeToString([]byte("too short"))
	if _, err := DecodeKey(short); err == nil {
		t.Fatal("expected error decoding short key, got nil")
	}
}

func TestDecodeKeyValid(t *testing.T) {
	raw := make([]byte, KeySize)
	rand.Read(raw)
	b64 := base64.StdEncoding.EncodeToString(raw)
	got, err := DecodeKey(b64)
	if err != nil {
		t.Fatalf("DecodeKey failed: %v", err)
	}
	if len(got) != KeySize {
		t.Fatalf("expected %d bytes, got %d", KeySize, len(got))
	}
}

func TestEncryptInvalidKey(t *testing.T) {
	if _, err := Encrypt([]byte("short"), []byte("x")); err == nil {
		t.Fatal("expected error with invalid key length")
	}
}
