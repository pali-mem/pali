package mock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmbedderProducesDeterministicSize(t *testing.T) {
	e := NewEmbedder()
	v1, err := e.Embed(context.Background(), "Go memory layer")
	require.NoError(t, err)
	require.Len(t, v1, 256)

	v2, err := e.Embed(context.Background(), "Go memory layer")
	require.NoError(t, err)
	require.Len(t, v2, 256)
	require.Equal(t, v1, v2)
}

func TestEmbedderIsStableAcrossOtherEmbeds(t *testing.T) {
	e := NewEmbedder()
	baseline, err := e.Embed(context.Background(), "Alice likes trail running")
	require.NoError(t, err)

	_, err = e.Embed(context.Background(), "Bob collects vintage maps")
	require.NoError(t, err)
	_, err = e.Embed(context.Background(), "Carol studies marine biology")
	require.NoError(t, err)

	after, err := e.Embed(context.Background(), "Alice likes trail running")
	require.NoError(t, err)
	require.Equal(t, baseline, after)
}

func TestBatchEmbedMatchesSingleEmbed(t *testing.T) {
	e := NewEmbedder()
	texts := []string{
		"Alice likes trail running",
		"Bob collects vintage maps",
	}

	batch, err := e.BatchEmbed(context.Background(), texts)
	require.NoError(t, err)
	require.Len(t, batch, len(texts))

	for i, text := range texts {
		single, err := e.Embed(context.Background(), text)
		require.NoError(t, err)
		require.Equal(t, single, batch[i])
	}
}
