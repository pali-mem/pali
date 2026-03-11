package memory

import (
	"container/list"
	"strings"
	"sync"
)

const defaultQueryEmbeddingCacheCapacity = 4096

type queryEmbeddingCache struct {
	mu       sync.Mutex
	capacity int
	ll       *list.List
	byKey    map[string]*list.Element
}

type queryEmbeddingCacheEntry struct {
	key       string
	embedding []float32
}

func newQueryEmbeddingCache(capacity int) *queryEmbeddingCache {
	if capacity <= 0 {
		return nil
	}
	return &queryEmbeddingCache{
		capacity: capacity,
		ll:       list.New(),
		byKey:    make(map[string]*list.Element, capacity),
	}
}

func (c *queryEmbeddingCache) Get(key string) ([]float32, bool) {
	if c == nil || strings.TrimSpace(key) == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.byKey[key]
	if !ok {
		return nil, false
	}
	c.ll.MoveToFront(elem)
	entry, ok := elem.Value.(*queryEmbeddingCacheEntry)
	if !ok || len(entry.embedding) == 0 {
		return nil, false
	}
	return cloneEmbedding(entry.embedding), true
}

func (c *queryEmbeddingCache) Set(key string, embedding []float32) {
	if c == nil || strings.TrimSpace(key) == "" || len(embedding) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.byKey[key]; ok {
		entry, _ := elem.Value.(*queryEmbeddingCacheEntry)
		if entry != nil {
			entry.embedding = cloneEmbedding(embedding)
		}
		c.ll.MoveToFront(elem)
		return
	}

	elem := c.ll.PushFront(&queryEmbeddingCacheEntry{
		key:       key,
		embedding: cloneEmbedding(embedding),
	})
	c.byKey[key] = elem
	for len(c.byKey) > c.capacity {
		back := c.ll.Back()
		if back == nil {
			return
		}
		entry, _ := back.Value.(*queryEmbeddingCacheEntry)
		if entry != nil {
			delete(c.byKey, entry.key)
		}
		c.ll.Remove(back)
	}
}

func normalizeQueryCacheKey(query string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(query)), " ")
	return strings.ToLower(normalized)
}

func (s *Service) getCachedQueryEmbedding(query string) ([]float32, bool) {
	if s == nil || s.queryCache == nil {
		return nil, false
	}
	key := normalizeQueryCacheKey(query)
	if key == "" {
		return nil, false
	}
	return s.queryCache.Get(key)
}

func (s *Service) setCachedQueryEmbedding(query string, embedding []float32) {
	if s == nil || s.queryCache == nil {
		return
	}
	key := normalizeQueryCacheKey(query)
	if key == "" {
		return
	}
	s.queryCache.Set(key, embedding)
}

func cloneEmbedding(embedding []float32) []float32 {
	if len(embedding) == 0 {
		return nil
	}
	out := make([]float32, len(embedding))
	copy(out, embedding)
	return out
}
