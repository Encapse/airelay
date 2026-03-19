package encrypt_test

import (
	"testing"

	"github.com/airelay/airelay/internal/encrypt"
	"github.com/stretchr/testify/require"
)

func TestRoundtrip(t *testing.T) {
	key := "my32byteencryptionkeyexactly123!"
	plaintext := "sk-real-openai-key-here"
	ciphertext, err := encrypt.Encrypt(key, plaintext)
	require.NoError(t, err)
	require.NotEqual(t, plaintext, ciphertext)

	decrypted, err := encrypt.Decrypt(key, ciphertext)
	require.NoError(t, err)
	require.Equal(t, plaintext, decrypted)
}

func TestDifferentOutputEachTime(t *testing.T) {
	key := "my32byteencryptionkeyexactly123!"
	c1, _ := encrypt.Encrypt(key, "same plaintext")
	c2, _ := encrypt.Encrypt(key, "same plaintext")
	require.NotEqual(t, c1, c2) // random nonce means different ciphertext
}

func TestWrongKeyFails(t *testing.T) {
	key1 := "my32byteencryptionkeyexactly123!"
	key2 := "different32byteencryptionkey12!!"
	ciphertext, _ := encrypt.Encrypt(key1, "secret")
	_, err := encrypt.Decrypt(key2, ciphertext)
	require.Error(t, err)
}

func TestEncrypt_WrongKeyLength(t *testing.T) {
	_, err := encrypt.Encrypt("tooshort", "payload")
	require.Error(t, err)
}
