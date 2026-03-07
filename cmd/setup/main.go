package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pali-mem/pali/internal/config"
)

const (
	defaultModelID        = "sentence-transformers/all-MiniLM-L6-v2"
	modelDir              = "models/all-MiniLM-L6-v2"
	runtimePathEnv        = "ONNXRUNTIME_SHARED_LIBRARY_PATH"
	defaultOllamaBaseURL  = "http://127.0.0.1:11434"
	defaultOllamaModel    = "mxbai-embed-large"
	defaultOllamaTimeout  = 2 * time.Second
	defaultOllamaTagsPath = "/api/tags"
)

type options struct {
	skipModelDownload bool
	downloadModel     bool
	skipRuntimeCheck  bool
	skipOllamaCheck   bool
	modelID           string
	ollamaBaseURL     string
	ollamaModel       string
}

func main() {
	opts := parseFlags()

	paths := []string{
		modelDir,
		"web/static/css",
		"web/static/js",
	}

	for _, p := range paths {
		if err := ensureDir(p); err != nil {
			fmt.Fprintf(os.Stderr, "failed creating %s: %v\n", p, err)
			os.Exit(1)
		}
	}

	if err := ensureConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "failed preparing pali.yaml: %v\n", err)
		os.Exit(1)
	}

	cfg, cfgErr := config.Load("pali.yaml")
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "failed reading pali.yaml for setup context: %v\n", cfgErr)
		os.Exit(1)
	}

	shouldDownloadModel := opts.downloadModel || strings.EqualFold(strings.TrimSpace(cfg.Embedding.Provider), "onnx")
	if opts.skipModelDownload {
		shouldDownloadModel = false
	}

	dlMessage := "skipped ONNX model download (use -download-model to prefetch for ONNX)"
	if shouldDownloadModel {
		if err := ensureModelArtifacts(opts.modelID); err != nil {
			fmt.Fprintf(os.Stderr, "failed ensuring model artifacts: %v\n", err)
			fmt.Fprintf(os.Stderr, "rerun with -skip-model-download for offline setup\n")
			os.Exit(1)
		}
		dlMessage = "ensured ONNX model/tokenizer files in " + modelDir
	}

	runtimeMessage := "skipped ONNX Runtime check (--skip-runtime-check)"
	if !opts.skipRuntimeCheck {
		if runtimePath, err := resolveRuntimeLibraryPath(); err != nil {
			runtimeMessage = "ONNX Runtime shared library not found (needed when embedding.provider=onnx)"
			printRuntimeInstallHint()
		} else {
			runtimeMessage = fmt.Sprintf("detected ONNX Runtime shared library reference: %s", runtimePath)
		}
	}

	ollamaMessage := "skipped Ollama check (--skip-ollama-check)"
	if !opts.skipOllamaCheck {
		ollamaBaseURL := strings.TrimSpace(opts.ollamaBaseURL)
		if ollamaBaseURL == "" {
			ollamaBaseURL = strings.TrimSpace(cfg.Embedding.OllamaBaseURL)
		}
		if ollamaBaseURL == "" {
			ollamaBaseURL = defaultOllamaBaseURL
		}
		ollamaModel := strings.TrimSpace(opts.ollamaModel)
		if ollamaModel == "" {
			ollamaModel = strings.TrimSpace(cfg.Embedding.OllamaModel)
		}
		if ollamaModel == "" {
			ollamaModel = defaultOllamaModel
		}
		if err := ensureOllamaReady(ollamaBaseURL, ollamaModel); err != nil {
			ollamaMessage = "Ollama embedder is not ready (needed when embedding.provider=ollama)"
			fmt.Println()
			fmt.Println(err.Error())
			fmt.Println()
		} else {
			ollamaMessage = fmt.Sprintf("detected Ollama + model %q at %s", ollamaModel, ollamaBaseURL)
		}
	}

	fmt.Println("Pali setup completed.")
	fmt.Println("- ensured directories under models/ and web/static/")
	fmt.Println("- ensured local config file pali.yaml (copied from pali.yaml.example when missing)")
	fmt.Printf("- %s\n", dlMessage)
	fmt.Printf("- %s\n", runtimeMessage)
	fmt.Printf("- %s\n", ollamaMessage)
	if !shouldDownloadModel {
		fmt.Println()
		fmt.Println("Tip: for higher accuracy embeddings, download the ONNX model:")
		fmt.Println("  go run ./cmd/setup -download-model")
	}
	fmt.Println("Next:")
	fmt.Println("1) run: go run ./cmd/pali -config pali.yaml")
	fmt.Println("2) open: http://127.0.0.1:8080/dashboard")
}

func parseFlags() options {
	var opts options
	flag.BoolVar(&opts.skipModelDownload, "skip-model-download", false, "Skip downloading ONNX model/tokenizer from Hugging Face")
	flag.BoolVar(&opts.downloadModel, "download-model", false, "Force ONNX model/tokenizer download even when provider is not onnx")
	flag.BoolVar(&opts.skipRuntimeCheck, "skip-runtime-check", false, "Skip checking ONNX Runtime shared library presence")
	flag.BoolVar(&opts.skipOllamaCheck, "skip-ollama-check", false, "Skip checking Ollama server/model readiness")
	flag.StringVar(&opts.modelID, "model-id", defaultModelID, "Hugging Face model id used for setup download")
	flag.StringVar(&opts.ollamaBaseURL, "ollama-base-url", "", "Ollama base URL for readiness checks (default from pali.yaml embedding.ollama_base_url)")
	flag.StringVar(&opts.ollamaModel, "ollama-model", "", "Ollama model name for readiness checks (default from pali.yaml embedding.ollama_model)")
	flag.Parse()
	return opts
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func ensureConfig() error {
	cfg := "pali.yaml"
	if _, err := os.Stat(cfg); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	example := "pali.yaml.example"
	src, err := os.ReadFile(example)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil && filepath.Dir(cfg) != "." {
		return err
	}

	return os.WriteFile(cfg, src, 0o644)
}

func ensureModelArtifacts(modelID string) error {
	files := []struct {
		remote string
		local  string
	}{
		{remote: "onnx/model.onnx", local: filepath.Join(modelDir, "model.onnx")},
		{remote: "tokenizer.json", local: filepath.Join(modelDir, "tokenizer.json")},
	}

	for _, f := range files {
		if fileExists(f.local) {
			continue
		}
		if err := downloadModelFile(modelID, f.remote, f.local); err != nil {
			return err
		}
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func downloadModelFile(modelID, remoteFile, localPath string) error {
	url := fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", strings.TrimSpace(modelID), remoteFile)

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", remoteFile, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: unexpected HTTP status %s", remoteFile, resp.Status)
	}

	tmpPath := localPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmpPath, err)
	}

	_, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write %s: %w", tmpPath, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close %s: %w", tmpPath, closeErr)
	}

	if err := os.Rename(tmpPath, localPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("finalize %s: %w", localPath, err)
	}

	if !fileExists(localPath) {
		return errors.New("download finished but target file is missing")
	}

	fmt.Printf("downloaded %s -> %s\n", remoteFile, localPath)
	return nil
}

func resolveRuntimeLibraryPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv(runtimePathEnv)); p != "" {
		if fileExists(p) || !strings.ContainsRune(p, filepath.Separator) {
			return p, nil
		}
	}

	for _, c := range defaultRuntimeCandidates() {
		if c == "" {
			continue
		}
		if fileExists(c) {
			return c, nil
		}
	}

	soname := defaultRuntimeSONAME()
	if soname != "" {
		if fileExists(soname) {
			return soname, nil
		}
	}
	return "", fmt.Errorf("ONNX Runtime shared library not found")
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

func printRuntimeInstallHint() {
	fmt.Println()
	fmt.Println("ONNX Runtime setup hint:")
	switch runtime.GOOS {
	case "darwin":
		fmt.Println("- macOS: brew install onnxruntime")
		fmt.Println("- then verify /opt/homebrew/lib/libonnxruntime.dylib or set ONNXRUNTIME_SHARED_LIBRARY_PATH")
	case "windows":
		fmt.Println("- Windows: download ONNX Runtime release zip and point ONNXRUNTIME_SHARED_LIBRARY_PATH to onnxruntime.dll")
		fmt.Println("- also install Microsoft Visual C++ 2019 runtime")
	default:
		fmt.Println("- download ONNX Runtime release archive and point ONNXRUNTIME_SHARED_LIBRARY_PATH to the shared library")
	}
	fmt.Println("- release binaries: https://github.com/microsoft/onnxruntime/releases")
	fmt.Println("- docs: https://onnxruntime.ai/docs/install/")
	fmt.Println()
}

func ensureOllamaReady(baseURL, model string) error {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultOllamaModel
	}

	client := &http.Client{Timeout: defaultOllamaTimeout}
	ctx := context.Background()
	if _, err := ollamaGET(ctx, client, baseURL+"/api/version"); err != nil {
		return fmt.Errorf("Ollama is not reachable at %s: %v\nInstall: https://ollama.com/download\nThen run:\n  1) ollama serve\n  2) ollama pull %s", baseURL, err, model)
	}

	body, err := ollamaGET(ctx, client, baseURL+defaultOllamaTagsPath)
	if err != nil {
		return fmt.Errorf("failed listing Ollama models at %s: %v\nRun: ollama pull %s", baseURL, err, model)
	}

	var parsed struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("invalid response from Ollama %s: %v", defaultOllamaTagsPath, err)
	}

	if !containsOllamaModel(parsed.Models, model) {
		return fmt.Errorf("Ollama model %q is missing\nRun: ollama pull %s", model, model)
	}

	return nil
}

func ollamaGET(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return body, nil
}

func containsOllamaModel(models []struct {
	Name string `json:"name"`
}, want string) bool {
	want = strings.TrimSpace(strings.ToLower(want))
	for _, m := range models {
		name := strings.TrimSpace(strings.ToLower(m.Name))
		if name == want || strings.HasPrefix(name, want+":") || strings.HasPrefix(want, name+":") {
			return true
		}
	}
	return false
}
