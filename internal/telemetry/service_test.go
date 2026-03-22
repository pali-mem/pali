package telemetry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServiceSnapshotTracksOperationsAndTopTenants(t *testing.T) {
	s := NewService(Options{
		RequestWindowMinutes: 5,
		EventBufferSize:      16,
	})

	now := time.Now().UTC()
	s.RequestStarted()
	s.RequestStarted()
	s.RequestFinished()

	s.RecordRequest(RequestObservation{
		At:      now,
		Method:  "POST",
		Path:    "/v1/memory/search",
		Status:  200,
		Latency: 45 * time.Millisecond,
	})
	s.RecordStore(StoreObservation{
		At:       now,
		TenantID: "tenant_a",
		Status:   201,
		Latency:  22 * time.Millisecond,
	})
	s.RecordBatchStore(BatchStoreObservation{
		At:           now,
		TenantWrites: map[string]int{"tenant_a": 2, "tenant_b": 1},
		Status:       201,
		Latency:      33 * time.Millisecond,
	})
	s.RecordSearch(SearchObservation{
		At:       now,
		TenantID: "tenant_b",
		Status:   200,
		Latency:  11 * time.Millisecond,
	})
	s.RecordSearch(SearchObservation{
		At:       now,
		TenantID: "tenant_a",
		Status:   500,
		Latency:  9 * time.Millisecond,
	})

	snapshot := s.Snapshot(SnapshotOptions{
		Events:     20,
		TopTenants: 5,
	})

	require.EqualValues(t, 1, snapshot.ActiveRequests)
	require.EqualValues(t, 1, snapshot.StoreCount)
	require.EqualValues(t, 1, snapshot.BatchStoreCount)
	require.EqualValues(t, 1, snapshot.SearchCount)
	require.Len(t, snapshot.RequestsPerMinute, 5)
	require.NotEmpty(t, snapshot.RecentEvents)

	require.Len(t, snapshot.TopTenantsByWrites, 2)
	require.Equal(t, "tenant_a", snapshot.TopTenantsByWrites[0].TenantID)
	require.EqualValues(t, 3, snapshot.TopTenantsByWrites[0].Writes)
	require.EqualValues(t, 1, snapshot.TopTenantsByWrites[1].Writes)

	require.Len(t, snapshot.TopTenantsBySearches, 2)
	require.Equal(t, "tenant_b", snapshot.TopTenantsBySearches[0].TenantID)
	require.EqualValues(t, 1, snapshot.TopTenantsBySearches[0].Searches)
}

func TestRequestsPerMinuteWindowAndSkips(t *testing.T) {
	s := NewService(Options{
		RequestWindowMinutes: 3,
		EventBufferSize:      4,
	})

	base := time.Date(2026, time.January, 2, 10, 5, 0, 0, time.UTC)
	s.RecordRequest(RequestObservation{
		At:      base,
		Method:  "POST",
		Path:    "/v1/memory",
		Status:  201,
		Latency: 14 * time.Millisecond,
	})
	s.RecordRequest(RequestObservation{
		At:      base,
		Method:  "POST",
		Path:    "/v1/memory",
		Status:  201,
		Latency: 18 * time.Millisecond,
	})
	s.RecordRequest(RequestObservation{
		At:      base.Add(time.Minute),
		Method:  "GET",
		Path:    "/dashboard/analytics/data",
		Status:  200,
		Latency: 3 * time.Millisecond,
	})
	s.RecordRequest(RequestObservation{
		At:      base.Add(time.Minute),
		Method:  "POST",
		Path:    "/v1/memory/search",
		Status:  200,
		Latency: 26 * time.Millisecond,
	})

	series := s.requestsPerMinute(base.Add(time.Minute))
	require.Len(t, series, 3)
	require.EqualValues(t, 0, series[0].Count)
	require.EqualValues(t, 2, series[1].Count)
	require.EqualValues(t, 1, series[2].Count)
}
