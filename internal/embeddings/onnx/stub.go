//go:build !onnx

// Package onnx provides a compile-time stub when the onnx build tag is absent.
package onnx

import (
	"errors"

	"github.com/pali-mem/pali/internal/domain"
)

// NewEmbedder is a compile-time stub returned when the binary is built without
// the "onnx" build tag. ONNX support is optional; compile with -tags onnx to
// enable it (requires the ONNX Runtime shared library at runtime).
func NewEmbedder(_, _ string) (domain.Embedder, error) {
	return nil, errors.New(
		"ONNX embedder is not compiled in; rebuild with -tags onnx and install the ONNX Runtime shared library (see docs/onnx.md)",
	)
}
