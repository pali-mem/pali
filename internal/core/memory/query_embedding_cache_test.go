package memory

import (
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewQueryEmbeddingCache_InvalidCapacityReturnsNil(t *testing.T) {
	require.Nil(t, newQueryEmbeddingCache(0))
	require.Nil(t, newQueryEmbeddingCache(-10))
}

func TestQueryEmbeddingCache_SetGetUsesClonedEmbedding(t *testing.T) {
	cache := newQueryEmbeddingCache(4)
	require.NotNil(t, cache)
	key := "Hello world"

	cache.Set(key, []float32{1, 2, 3})

	got, ok := cache.Get(key)
	require.True(t, ok)
	require.Equal(t, []float32{1, 2, 3}, got)

	got[0] = 9
	reloaded, ok := cache.Get(key)
	require.True(t, ok)
	require.Equal(t, float32(1), reloaded[0], "cache should return cloned embeddings")
}

func TestQueryEmbeddingCache_EvictionHonorsLRU(t *testing.T) {
	cache := newQueryEmbeddingCache(2)
	require.NotNil(t, cache)

	cache.Set("one", []float32{1})
	cache.Set("two", []float32{2})
	_, _ = cache.Get("one")
	cache.Set("three", []float32{3})

	_, ok := cache.Get("two")
	require.False(t, ok)
	_, ok = cache.Get("one")
	require.True(t, ok)
	_, ok = cache.Get("three")
	require.True(t, ok)
	require.Len(t, cache.byKey, 2)
}

func TestQueryEmbeddingCache_ConcurrentGetSet(t *testing.T) {
	cache := newQueryEmbeddingCache(32)
	require.NotNil(t, cache)

	var wg sync.WaitGroup

	for i := 0; i < 64; i++ {
		wg.Add(2)
		i := i
		go func() {
			defer wg.Done()
			key := "k_" + strconv.Itoa(i)
			cache.Set(key, []float32{float32(i)})
		}()
		go func() {
			defer wg.Done()
			key := "k_" + strconv.Itoa(i)
			_, _ = cache.Get(key)
		}()
	}

	wg.Wait()
	cache.Set("anchor", []float32{99})
	got, ok := cache.Get("anchor")
	require.True(t, ok)
	require.Equal(t, float32(99), got[0])
}
