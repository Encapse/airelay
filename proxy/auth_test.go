package proxy_test

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/airelay/airelay/proxy"
	"github.com/stretchr/testify/require"
)

func TestHashKey(t *testing.T) {
	key := "air_sk_testkey123"
	hash := proxy.HashKey(key)
	expected := fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
	require.Equal(t, expected, hash)
}

func TestGenerateKey(t *testing.T) {
	full, prefix, hash := proxy.GenerateKey()
	require.True(t, strings.HasPrefix(full, "air_sk_"), "key must start with air_sk_")
	require.True(t, len(full) > 20)
	require.True(t, len(full) >= 16)
	require.Equal(t, full[:16], prefix)
	require.Equal(t, proxy.HashKey(full), hash)
}

func TestGenerateKey_Unique(t *testing.T) {
	_, _, h1 := proxy.GenerateKey()
	_, _, h2 := proxy.GenerateKey()
	require.NotEqual(t, h1, h2)
}
