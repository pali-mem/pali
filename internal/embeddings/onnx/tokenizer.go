package onnx

import (
	"fmt"
	"os"
	"strings"

	"github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
)

func LoadTokenizer(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("tokenizer path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("tokenizer file not found: %w", err)
	}
	return nil
}

func loadTokenizer(path string) (*tokenizer.Tokenizer, error) {
	if err := LoadTokenizer(path); err != nil {
		return nil, err
	}

	tk, err := pretrained.FromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer from file: %w", err)
	}
	return tk, nil
}
