// Package secret provides AES-256-GCM symmetric encryption for sensitive
// values stored at rest (e.g. an agent's LLM API key, custom env var
// values).
//
// A near-verbatim copy of services/api/internal/platform/secret/encryptor.go
// — not imported (that package is under services/api/internal, unreachable
// from this sibling module) but not reimplemented from scratch either: the
// two processes share one ENCRYPTION_KEY, so this must byte-for-byte match
// services/api's Encrypt/Decrypt or a value one service wrote becomes
// undecryptable by the other. Any change to the wire format there must be
// mirrored here in the same change.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// ErrInvalidKeyLength is returned when the supplied key is not 32 bytes.
var ErrInvalidKeyLength = errors.New("secret: encryption key must be exactly 32 bytes (AES-256)")

// DecodeHexKey decodes a 64-character hex string into the 32-byte key
// expected by NewEncryptor. Matches the ENCRYPTION_KEY environment variable
// format shared with services/api.
func DecodeHexKey(hexKey string) ([]byte, error) {
	b, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("secret: decode hex key: %w", err)
	}
	if len(b) != 32 {
		return nil, ErrInvalidKeyLength
	}
	return b, nil
}

// Encryptor encrypts and decrypts short strings using AES-256-GCM.
type Encryptor struct {
	key []byte
}

// NewEncryptor creates an Encryptor from a 32-byte key.
func NewEncryptor(key []byte) (*Encryptor, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKeyLength
	}
	k := make([]byte, 32)
	copy(k, key)
	return &Encryptor{key: k}, nil
}

// Encrypt encrypts plaintext and returns a base64-encoded string consisting
// of the random nonce followed by the GCM ciphertext+tag.
func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("secret: create cipher block: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("secret: create GCM wrapper: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("secret: generate nonce: %w", err)
	}

	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt decrypts a base64-encoded ciphertext produced by Encrypt (by
// either this process or services/api — see the package doc comment).
func (e *Encryptor) Decrypt(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("secret: base64 decode: %w", err)
	}

	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("secret: create cipher block: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("secret: create GCM wrapper: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("secret: ciphertext is too short to contain a nonce")
	}

	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("secret: GCM open (authenticate+decrypt): %w", err)
	}

	return string(plaintext), nil
}
