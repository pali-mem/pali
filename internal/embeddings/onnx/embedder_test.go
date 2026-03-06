package onnx

import (
	"testing"

	"github.com/stretchr/testify/require"
	ort "github.com/yalue/onnxruntime_go"
)

func TestSelectInputNames(t *testing.T) {
	inputs := []ort.InputOutputInfo{
		{Name: "weird_input"},
		{Name: inputNameMask},
	}
	names, err := selectInputNames(inputs)
	require.Error(t, err)
	require.Nil(t, names)
}

func TestSelectInputNamesSuccess(t *testing.T) {
	inputs := []ort.InputOutputInfo{
		{Name: inputNameIDs},
		{Name: inputNameMask},
		{Name: inputNameTokenTypeIDs},
	}
	names, err := selectInputNames(inputs)
	require.NoError(t, err)
	require.Equal(t, []string{inputNameIDs, inputNameMask, inputNameTokenTypeIDs}, names)
}

func TestSelectOutputName(t *testing.T) {
	name, err := selectOutputName([]ort.InputOutputInfo{{Name: "foo"}, {Name: "last_hidden_state"}})
	require.NoError(t, err)
	require.Equal(t, "last_hidden_state", name)
}

func TestMeanPoolAndNormalize(t *testing.T) {
	// 3 tokens, hidden=2
	hidden := []float32{
		1, 2,
		3, 4,
		9, 9, // masked out
	}
	mask := []int{1, 1, 0}

	vec, err := meanPoolAndNormalize(hidden, mask)
	require.NoError(t, err)
	require.Len(t, vec, 2)
	// Mean of active tokens = [2,3], normalized.
	require.InDelta(t, 0.5547, vec[0], 0.01)
	require.InDelta(t, 0.8320, vec[1], 0.01)
}
