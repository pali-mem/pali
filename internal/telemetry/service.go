package telemetry

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultRequestWindowMinutes = 30
	defaultEventBufferSize      = 256
	defaultRecentEventLimit     = 20
	defaultTopTenantLimit       = 5

	// TenantContextKey is used to pass resolved tenant IDs from handlers to middleware telemetry.
	TenantContextKey = "telemetry_tenant_id"
)

type Options struct {
	RequestWindowMinutes int
	EventBufferSize      int
}

type SnapshotOptions struct {
	Events     int
	TopTenants int
}

type MinuteCount struct {
	Minute time.Time `json:"minute"`
	Count  int64     `json:"count"`
}

type Event struct {
	At        time.Time `json:"at"`
	Type      string    `json:"type"`
	Method    string    `json:"method,omitempty"`
	Path      string    `json:"path,omitempty"`
	TenantID  string    `json:"tenant_id,omitempty"`
	Status    int       `json:"status"`
	LatencyMS int64     `json:"latency_ms"`
	Detail    string    `json:"detail,omitempty"`
}

type TenantStat struct {
	TenantID string `json:"tenant_id"`
	Writes   int64  `json:"writes"`
	Searches int64  `json:"searches"`
}

type Snapshot struct {
	GeneratedAt          time.Time     `json:"generated_at"`
	ActiveRequests       int64         `json:"active_requests"`
	StoreCount           int64         `json:"store_count"`
	BatchStoreCount      int64         `json:"batch_store_count"`
	SearchCount          int64         `json:"search_count"`
	RequestsPerMinute    []MinuteCount `json:"requests_per_minute"`
	RecentEvents         []Event       `json:"recent_events"`
	TopTenantsByWrites   []TenantStat  `json:"top_tenants_by_writes"`
	TopTenantsBySearches []TenantStat  `json:"top_tenants_by_searches"`
}

type RequestObservation struct {
	At       time.Time
	Method   string
	Path     string
	TenantID string
	Status   int
	Latency  time.Duration
}

type StoreObservation struct {
	At       time.Time
	TenantID string
	Status   int
	Latency  time.Duration
}

type BatchStoreObservation struct {
	At           time.Time
	TenantWrites map[string]int
	Status       int
	Latency      time.Duration
}

type SearchObservation struct {
	At       time.Time
	TenantID string
	Status   int
	Latency  time.Duration
}

type minuteBucket struct {
	Minute int64
	Count  int64
}

type tenantCounts struct {
	Writes   int64
	Searches int64
}

type Service struct {
	activeRequests  atomic.Int64
	storeCount      atomic.Int64
	batchStoreCount atomic.Int64
	searchCount     atomic.Int64

	requestMu   sync.Mutex
	requestRing []minuteBucket

	eventMu   sync.Mutex
	events    []Event
	eventHead int
	eventSize int

	tenantMu     sync.Mutex
	tenantCounts map[string]*tenantCounts
}

func NewService(opts Options) *Service {
	window := opts.RequestWindowMinutes
	if window <= 0 {
		window = defaultRequestWindowMinutes
	}
	eventCap := opts.EventBufferSize
	if eventCap <= 0 {
		eventCap = defaultEventBufferSize
	}

	return &Service{
		requestRing:  make([]minuteBucket, window),
		events:       make([]Event, eventCap),
		tenantCounts: make(map[string]*tenantCounts),
	}
}

func (s *Service) RequestStarted() {
	if s == nil {
		return
	}
	s.activeRequests.Add(1)
}

func (s *Service) RequestFinished() {
	if s == nil {
		return
	}
	s.activeRequests.Add(-1)
}

func (s *Service) RecordRequest(obs RequestObservation) {
	if s == nil {
		return
	}
	now := obs.At
	if now.IsZero() {
		now = time.Now().UTC()
	}
	path := strings.TrimSpace(obs.Path)
	if path == "" {
		path = "/"
	}

	if !shouldSkipRequestCount(path) {
		s.recordRequestMinute(now)
	}
	if shouldRecordRequestEvent(path) {
		s.appendEvent(Event{
			At:        now,
			Type:      "request",
			Method:    strings.TrimSpace(obs.Method),
			Path:      path,
			TenantID:  strings.TrimSpace(obs.TenantID),
			Status:    obs.Status,
			LatencyMS: obs.Latency.Milliseconds(),
		})
	}
}

func (s *Service) RecordStore(obs StoreObservation) {
	if s == nil {
		return
	}
	now := obs.At
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tenantID := strings.TrimSpace(obs.TenantID)
	if isSuccessStatus(obs.Status) {
		s.storeCount.Add(1)
		s.addTenantWrites(tenantID, 1)
	}
	s.appendEvent(Event{
		At:        now,
		Type:      "store",
		Method:    "POST",
		Path:      "/v1/memory",
		TenantID:  tenantID,
		Status:    obs.Status,
		LatencyMS: obs.Latency.Milliseconds(),
	})
}

func (s *Service) RecordBatchStore(obs BatchStoreObservation) {
	if s == nil {
		return
	}
	now := obs.At
	if now.IsZero() {
		now = time.Now().UTC()
	}

	totalItems := 0
	tenantID := ""
	tenantCount := 0
	for rawTenantID, count := range obs.TenantWrites {
		if count <= 0 {
			continue
		}
		id := strings.TrimSpace(rawTenantID)
		if id == "" {
			continue
		}
		totalItems += count
		tenantCount++
		tenantID = id
		if isSuccessStatus(obs.Status) {
			s.addTenantWrites(id, int64(count))
		}
	}
	if !isSuccessStatus(obs.Status) {
		totalItems = 0
	}
	if tenantCount != 1 {
		tenantID = ""
	}
	if isSuccessStatus(obs.Status) {
		s.batchStoreCount.Add(1)
	}

	detail := ""
	if totalItems > 0 {
		detail = "items=" + strconv.Itoa(totalItems)
	}
	s.appendEvent(Event{
		At:        now,
		Type:      "batch_store",
		Method:    "POST",
		Path:      "/v1/memory/batch",
		TenantID:  tenantID,
		Status:    obs.Status,
		LatencyMS: obs.Latency.Milliseconds(),
		Detail:    detail,
	})
}

func (s *Service) RecordSearch(obs SearchObservation) {
	if s == nil {
		return
	}
	now := obs.At
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tenantID := strings.TrimSpace(obs.TenantID)
	if isSuccessStatus(obs.Status) {
		s.searchCount.Add(1)
		s.addTenantSearches(tenantID, 1)
	}
	s.appendEvent(Event{
		At:        now,
		Type:      "search",
		Method:    "POST",
		Path:      "/v1/memory/search",
		TenantID:  tenantID,
		Status:    obs.Status,
		LatencyMS: obs.Latency.Milliseconds(),
	})
}

func (s *Service) Snapshot(opts SnapshotOptions) Snapshot {
	if s == nil {
		return Snapshot{
			GeneratedAt:          time.Now().UTC(),
			RequestsPerMinute:    []MinuteCount{},
			RecentEvents:         []Event{},
			TopTenantsByWrites:   []TenantStat{},
			TopTenantsBySearches: []TenantStat{},
		}
	}

	eventLimit := opts.Events
	if eventLimit <= 0 {
		eventLimit = defaultRecentEventLimit
	}
	topN := opts.TopTenants
	if topN <= 0 {
		topN = defaultTopTenantLimit
	}

	now := time.Now().UTC()
	return Snapshot{
		GeneratedAt:          now,
		ActiveRequests:       s.activeRequests.Load(),
		StoreCount:           s.storeCount.Load(),
		BatchStoreCount:      s.batchStoreCount.Load(),
		SearchCount:          s.searchCount.Load(),
		RequestsPerMinute:    s.requestsPerMinute(now),
		RecentEvents:         s.recentEvents(eventLimit),
		TopTenantsByWrites:   s.topTenants(topN, true),
		TopTenantsBySearches: s.topTenants(topN, false),
	}
}

func (s *Service) requestsPerMinute(now time.Time) []MinuteCount {
	s.requestMu.Lock()
	defer s.requestMu.Unlock()

	window := len(s.requestRing)
	out := make([]MinuteCount, 0, window)
	if window == 0 {
		return out
	}
	currentMinute := now.Unix() / 60
	startMinute := currentMinute - int64(window) + 1
	for minute := startMinute; minute <= currentMinute; minute++ {
		idx := int(minute % int64(window))
		count := int64(0)
		if s.requestRing[idx].Minute == minute {
			count = s.requestRing[idx].Count
		}
		out = append(out, MinuteCount{
			Minute: time.Unix(minute*60, 0).UTC(),
			Count:  count,
		})
	}
	return out
}

func (s *Service) recentEvents(limit int) []Event {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()

	if limit <= 0 {
		limit = defaultRecentEventLimit
	}
	if limit > s.eventSize {
		limit = s.eventSize
	}
	out := make([]Event, 0, limit)
	for i := 0; i < limit; i++ {
		idx := (s.eventHead - 1 - i + len(s.events)) % len(s.events)
		out = append(out, s.events[idx])
	}
	return out
}

func (s *Service) topTenants(limit int, sortByWrites bool) []TenantStat {
	s.tenantMu.Lock()
	defer s.tenantMu.Unlock()

	if limit <= 0 {
		limit = defaultTopTenantLimit
	}
	out := make([]TenantStat, 0, len(s.tenantCounts))
	for id, counts := range s.tenantCounts {
		if counts == nil {
			continue
		}
		out = append(out, TenantStat{
			TenantID: id,
			Writes:   counts.Writes,
			Searches: counts.Searches,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if sortByWrites {
			if left.Writes != right.Writes {
				return left.Writes > right.Writes
			}
			if left.Searches != right.Searches {
				return left.Searches > right.Searches
			}
		} else {
			if left.Searches != right.Searches {
				return left.Searches > right.Searches
			}
			if left.Writes != right.Writes {
				return left.Writes > right.Writes
			}
		}
		return left.TenantID < right.TenantID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Service) recordRequestMinute(now time.Time) {
	s.requestMu.Lock()
	defer s.requestMu.Unlock()

	window := len(s.requestRing)
	if window == 0 {
		return
	}
	minute := now.Unix() / 60
	idx := int(minute % int64(window))
	bucket := &s.requestRing[idx]
	if bucket.Minute != minute {
		bucket.Minute = minute
		bucket.Count = 1
		return
	}
	bucket.Count++
}

func (s *Service) addTenantWrites(tenantID string, value int64) {
	if value <= 0 || tenantID == "" {
		return
	}
	s.tenantMu.Lock()
	defer s.tenantMu.Unlock()
	counts := s.tenantCounts[tenantID]
	if counts == nil {
		counts = &tenantCounts{}
		s.tenantCounts[tenantID] = counts
	}
	counts.Writes += value
}

func (s *Service) addTenantSearches(tenantID string, value int64) {
	if value <= 0 || tenantID == "" {
		return
	}
	s.tenantMu.Lock()
	defer s.tenantMu.Unlock()
	counts := s.tenantCounts[tenantID]
	if counts == nil {
		counts = &tenantCounts{}
		s.tenantCounts[tenantID] = counts
	}
	counts.Searches += value
}

func (s *Service) appendEvent(event Event) {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()

	if len(s.events) == 0 {
		return
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	s.events[s.eventHead] = event
	s.eventHead = (s.eventHead + 1) % len(s.events)
	if s.eventSize < len(s.events) {
		s.eventSize++
	}
}

func isSuccessStatus(status int) bool {
	return status >= 200 && status < 400
}

func shouldSkipRequestCount(path string) bool {
	return !shouldTrackRequestPath(path)
}

func shouldRecordRequestEvent(path string) bool {
	return shouldTrackRequestPath(path)
}

func shouldTrackRequestPath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/v1/")
}

func ShouldTrackRequestPath(path string) bool {
	return shouldTrackRequestPath(path)
}
