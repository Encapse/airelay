package proxy_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/airelay/airelay/proxy"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSpendKey_Daily(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	day := time.Date(2026, 3, 17, 12, 0, 0, 0, time.UTC)
	key := proxy.SpendKey(id, "daily", day)
	require.Equal(t, fmt.Sprintf("spend:%s:daily:2026-03-17", id), key)
}

func TestSpendKey_Monthly(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	day := time.Date(2026, 3, 17, 12, 0, 0, 0, time.UTC)
	key := proxy.SpendKey(id, "monthly", day)
	require.Equal(t, fmt.Sprintf("spend:%s:monthly:2026-03", id), key)
}
