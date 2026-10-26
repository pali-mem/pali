package handlers

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestMemoryResponseOmitsLastRecalledAtWhenNeverRecalled(t *testing.T) {
	raw, err := json.Marshal(toMemoryResponse(domain.Memory{ID: "mem_1", TenantID: "t"}))
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	require.NotContains(t, out, "last_recalled_at")

	recalled := time.Date(2026, 3, 5, 4, 20, 0, 0, time.UTC)
	raw, err = json.Marshal(toMemoryResponse(domain.Memory{ID: "mem_1", TenantID: "t", LastRecalledAt: recalled}))
	require.NoError(t, err)
	out = map[string]any{}
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Equal(t, "2026-03-05T04:20:00Z", out["last_recalled_at"])
}
