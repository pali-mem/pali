// Package pali exposes repository-level files embedded into the binary.
package pali

import _ "embed"

// ExampleConfig is pali.yaml.example. `pali init` writes it when no example
// file exists in the working directory (for example after a release install).
//
//go:embed pali.yaml.example
var ExampleConfig []byte
