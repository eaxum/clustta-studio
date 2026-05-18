// Package cryptoutil provides authenticated symmetric encryption helpers
// used to protect secrets at rest in the central server database.
//
// All operations use AES-256-GCM with a 12-byte random nonce. The encoded
// output is base64(nonce || ciphertext || authTag) so a single TEXT column
// is sufficient for storage.
package cryptoutil

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

// KeySize is the required length, in bytes, of the master key used by
// Encrypt and Decrypt. AES-256 is the only supported variant.
const KeySize = 32

// ErrInvalidKey is returned when DecodeKey is given a value that does not
// decode to exactly KeySize bytes.
var ErrInvalidKey = errors.New("integration secret key must decode to 32 bytes")

// ErrCiphertextTooShort is returned by Decrypt when the input cannot
// possibly contain a nonce and ciphertext.
var ErrCiphertextTooShort = errors.New("ciphertext too short")

// DecodeKey decodes a base64-encoded master key and verifies its length.
// The returned slice should be held in process memory and never logged.
func DecodeKey(b64 string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	if len(raw) != KeySize {
		return nil, ErrInvalidKey
	}
	return raw, nil
}

// Encrypt seals plaintext using AES-256-GCM and returns the base64-encoded
// (nonce || ciphertext || authTag) blob suitable for storage in a TEXT column.
func Encrypt(key, plaintext []byte) (string, error) {
	if len(key) != KeySize {
		return "", ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. It returns an error if the ciphertext was
// tampered with, the wrong key was provided, or the input is malformed.
func Decrypt(key []byte, b64 string) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	sealed, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, ErrCiphertextTooShort
	}
	nonce, body := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	return gcm.Open(nil, nonce, body, nil)
}
