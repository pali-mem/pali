package testutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AssertNoError is a thin wrapper kept for backwards compatibility.
// Prefer require.NoError(t, err) directly in new tests.
func AssertNoError(t *testing.T, err error) {
	t.Helper()
	require.NoError(t, err)
}

// AssertEqual wraps assert.Equal for convenience.
func AssertEqual[T any](t *testing.T, expected, actual T) {
	t.Helper()
	assert.Equal(t, expected, actual)
}
