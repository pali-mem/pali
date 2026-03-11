//go:build onnx

package onnx

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

const runtimePathEnv = "ONNXRUNTIME_SHARED_LIBRARY_PATH"

var (
	runtimeMu          sync.Mutex
	runtimeInitialized bool
)

func ensureRuntimeInitialized() error {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()

	if runtimeInitialized {
		return nil
	}

	libPath, err := resolveRuntimeLibraryPath()
	if err != nil {
		return err
	}
	ort.SetSharedLibraryPath(libPath)

	if err := ort.InitializeEnvironment(); err != nil {
		return fmt.Errorf("initialize onnx runtime (path=%q): %w", libPath, err)
	}

	runtimeInitialized = true
	return nil
}

func resolveRuntimeLibraryPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv(runtimePathEnv)); p != "" {
		return p, nil
	}

	candidates := defaultRuntimeCandidates()
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	soname := defaultRuntimeSONAME()
	if soname == "" {
		return "", fmt.Errorf("unsupported platform for ONNX runtime (%s/%s)", runtime.GOOS, runtime.GOARCH)
	}
	return soname, nil
}

func defaultRuntimeCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/opt/homebrew/lib/libonnxruntime.dylib",
			"/usr/local/lib/libonnxruntime.dylib",
			filepath.Join(".", "libonnxruntime.dylib"),
		}
	case "linux":
		return []string{
			"/usr/local/lib/libonnxruntime.so",
			"/usr/lib/libonnxruntime.so",
			filepath.Join(".", "libonnxruntime.so"),
			filepath.Join(".", "onnxruntime.so"),
		}
	case "windows":
		return []string{
			`C:\Program Files\onnxruntime\bin\onnxruntime.dll`,
			`C:\onnxruntime\bin\onnxruntime.dll`,
			"onnxruntime.dll",
		}
	default:
		return nil
	}
}

func defaultRuntimeSONAME() string {
	switch runtime.GOOS {
	case "darwin":
		return "libonnxruntime.dylib"
	case "linux":
		return "onnxruntime.so"
	case "windows":
		return "onnxruntime.dll"
	default:
		return ""
	}
}
