package dashboard

import (
	"testing"
	"time"

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

func TestFormatOptionalDashboardTime_NeverForUnsetTimes(t *testing.T) {
	require.Equal(t, "Never", formatOptionalDashboardTime(time.Time{}))
	require.Equal(t, "Never", formatOptionalDashboardTime(time.Unix(0, 0)))
	require.Equal(t, "2026-10-25 12:30", formatOptionalDashboardTime(time.Date(2026, 10, 25, 12, 30, 0, 0, time.UTC)))
}

func TestSortedCounts_OrdersByCountThenLabel(t *testing.T) {
	got := sortedCounts(map[string]int64{"b": 2, "a": 2, "c": 5})
	require.Equal(t, []CountView{{"c", 5}, {"a", 2}, {"b", 2}}, got)
	require.Empty(t, sortedCounts(nil))
}

func TestBuildMemoriesPageURL_PreservesFilters(t *testing.T) {
	filters := MemoryFilterState{SelectedTenantID: "t1", SelectedTier: "semantic", SelectedKind: "event"}
	require.Equal(t, "/dashboard/memories?kind=event&tenant_id=t1&tier=semantic", buildMemoriesPageURL(filters, 0))
	require.Equal(t, "/dashboard/memories?kind=event&tenant_id=t1&tier=semantic&offset=50", buildMemoriesPageURL(filters, 50))
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
