package onnx

import (
	"fmt"
	"os"
	"strings"
)

func LoadModel(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("model path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("model file not found: %w", err)
	}
	return nil
}
