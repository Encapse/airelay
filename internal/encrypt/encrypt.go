package encrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// deriveKey returns a 32-byte AES key from an arbitrary-length string key.
func deriveKey(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}

// Encrypt encrypts plaintext with AES-256-GCM using a random nonce.
// Returns base64(nonce + ciphertext). Key must be at least 16 characters.
func Encrypt(key, plaintext string) (string, error) {
	if len(key) < 16 {
		return "", fmt.Errorf("encryption key must be at least 16 characters")
	}
	block, err := aes.NewCipher(deriveKey(key))
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt decrypts a base64-encoded AES-256-GCM ciphertext.
func Decrypt(key, ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	block, err := aes.NewCipher(deriveKey(key))
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, sealed := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}
