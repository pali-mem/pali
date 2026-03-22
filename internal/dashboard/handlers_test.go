package dashboard

import (
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTopTenantsByMemory_LimitsAndSorts(t *testing.T) {
	items := []TenantView{
		{ID: "tenant_c", MemoryCount: 3},
		{ID: "tenant_a", MemoryCount: 8},
		{ID: "tenant_b", MemoryCount: 8},
		{ID: "tenant_d", MemoryCount: 1},
	}

	top := topTenantsByMemory(items, 3)
	require.Len(t, top, 3)
	require.Equal(t, "tenant_a", top[0].ID)
	require.Equal(t, "tenant_b", top[1].ID)
	require.Equal(t, "tenant_c", top[2].ID)

	require.Len(t, items, 4)
	require.Equal(t, "tenant_c", items[0].ID)
}

func TestFilterMemoriesByLiteralQuery(t *testing.T) {
	items := []domain.Memory{
		{
			ID:      "m1",
			Content: "User likes bananas with oats.",
			Tags:    []string{"food"},
		},
		{
			ID:      "m2",
			Content: "User prefers tea in the morning.",
			Tags:    []string{"drink"},
		},
	}

	got := filterMemoriesByLiteralQuery(items, "banana")
	require.Len(t, got, 1)
	require.Equal(t, "m1", got[0].ID)

	got = filterMemoriesByLiteralQuery(items, "tea morning")
	require.Len(t, got, 1)
	require.Equal(t, "m2", got[0].ID)

	got = filterMemoriesByLiteralQuery(items, "nonexistent")
	require.Empty(t, got)
}
