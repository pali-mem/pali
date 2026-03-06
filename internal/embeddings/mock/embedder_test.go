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
}
