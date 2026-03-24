// Package bootstrap prepares local model assets and validates setup prerequisites.
package bootstrap

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

// Options configures the setup workflow.
type Options struct {
	ConfigPath        string
	SkipModelDownload bool
	DownloadModel     bool
	SkipRuntimeCheck  bool
	SkipOllamaCheck   bool
	ModelID           string
	OllamaBaseURL     string
	OllamaModel       string
}

// DefaultOptions returns the setup defaults.
func DefaultOptions() Options {
	return Options{
		ConfigPath: "pali.yaml",
		ModelID:    defaultModelID,
	}
}

// AddFlags binds setup flags to the provided flag set.
func AddFlags(fs *flag.FlagSet, opts *Options) {
	fs.StringVar(&opts.ConfigPath, "config", "pali.yaml", "Config file path to create/read during setup")
	fs.BoolVar(&opts.SkipModelDownload, "skip-model-download", false, "Skip downloading ONNX model/tokenizer from Hugging Face")
	fs.BoolVar(&opts.DownloadModel, "download-model", false, "Force ONNX model/tokenizer download even when provider is not onnx")
	fs.BoolVar(&opts.SkipRuntimeCheck, "skip-runtime-check", false, "Skip checking ONNX Runtime shared library presence")
	fs.BoolVar(&opts.SkipOllamaCheck, "skip-ollama-check", false, "Skip checking Ollama server/model readiness")
	fs.StringVar(&opts.ModelID, "model-id", defaultModelID, "Hugging Face model id used for setup download")
	fs.StringVar(&opts.OllamaBaseURL, "ollama-base-url", "", "Ollama base URL for readiness checks (default from config embedding.ollama_base_url)")
	fs.StringVar(&opts.OllamaModel, "ollama-model", "", "Ollama model name for readiness checks (default from config embedding.ollama_model)")
}

// Run executes the setup workflow.
func Run(opts Options, stdout, stderr io.Writer) error {
	paths := []string{
		modelDir,
		"web/static/css",
		"web/static/js",
	}

	for _, p := range paths {
		if err := ensureDir(p); err != nil {
			return fmt.Errorf("failed creating %s: %w", p, err)
		}
	}

	configPath := strings.TrimSpace(opts.ConfigPath)
	if configPath == "" {
		configPath = "pali.yaml"
	}

	if err := EnsureConfig(configPath); err != nil {
		return fmt.Errorf("failed preparing %s: %w", configPath, err)
	}

	cfg, cfgErr := config.Load(configPath)
	if cfgErr != nil {
		return fmt.Errorf("failed reading %s for setup context: %w", configPath, cfgErr)
	}

	shouldDownloadModel := opts.DownloadModel || strings.EqualFold(strings.TrimSpace(cfg.Embedding.Provider), "onnx")
	if opts.SkipModelDownload {
		shouldDownloadModel = false
	}

	dlMessage := "skipped ONNX model download (use -download-model to prefetch for ONNX)"
	if shouldDownloadModel {
		if err := ensureModelArtifacts(opts.ModelID); err != nil {
			_, _ = fmt.Fprintf(stderr, "rerun with -skip-model-download for offline setup\n")
			return fmt.Errorf("failed ensuring model artifacts: %w", err)
		}
		dlMessage = "ensured ONNX model/tokenizer files in " + modelDir
	}

	runtimeMessage := "skipped ONNX Runtime check (--skip-runtime-check)"
	if !opts.SkipRuntimeCheck {
		if runtimePath, err := resolveRuntimeLibraryPath(); err != nil {
			runtimeMessage = "ONNX Runtime shared library not found (needed when embedding.provider=onnx)"
			printRuntimeInstallHint(stdout)
		} else {
			runtimeMessage = fmt.Sprintf("detected ONNX Runtime shared library reference: %s", runtimePath)
		}
	}

	ollamaMessage := "skipped Ollama check (--skip-ollama-check)"
	if !opts.SkipOllamaCheck {
		needsOllama := strings.EqualFold(strings.TrimSpace(cfg.Embedding.Provider), "ollama") ||
			strings.EqualFold(strings.TrimSpace(cfg.ImportanceScorer), "ollama") ||
			(cfg.Parser.Enabled && strings.EqualFold(strings.TrimSpace(cfg.Parser.Provider), "ollama")) ||
			strings.EqualFold(strings.TrimSpace(cfg.Retrieval.MultiHop.DecompositionProvider), "ollama")
		if !needsOllama {
			ollamaMessage = "skipped Ollama check (ollama provider not enabled in current config)"
		}
		if needsOllama {
			ollamaBaseURL := strings.TrimSpace(opts.OllamaBaseURL)
			if ollamaBaseURL == "" {
				ollamaBaseURL = strings.TrimSpace(cfg.Embedding.OllamaBaseURL)
			}
			if ollamaBaseURL == "" {
				ollamaBaseURL = defaultOllamaBaseURL
			}
			ollamaModel := strings.TrimSpace(opts.OllamaModel)
			if ollamaModel == "" {
				ollamaModel = strings.TrimSpace(cfg.Embedding.OllamaModel)
			}
			if ollamaModel == "" {
				ollamaModel = defaultOllamaModel
			}
			if err := ensureOllamaReady(ollamaBaseURL, ollamaModel); err != nil {
				ollamaMessage = "Ollama embedder is not ready (needed when an Ollama-backed component is enabled)"
				_, _ = fmt.Fprintln(stdout)
				_, _ = fmt.Fprintln(stdout, err.Error())
				_, _ = fmt.Fprintln(stdout)
			} else {
				ollamaMessage = fmt.Sprintf("detected Ollama + model %q at %s", ollamaModel, ollamaBaseURL)
			}
		}
	}

	_, _ = fmt.Fprintln(stdout, "Pali setup completed.")
	_, _ = fmt.Fprintln(stdout, "- ensured directories under models/ and web/static/")
	_, _ = fmt.Fprintf(stdout, "- ensured config file %s (copied from pali.yaml.example when missing)\n", configPath)
	_, _ = fmt.Fprintf(stdout, "- %s\n", dlMessage)
	_, _ = fmt.Fprintf(stdout, "- %s\n", runtimeMessage)
	_, _ = fmt.Fprintf(stdout, "- %s\n", ollamaMessage)
	if !shouldDownloadModel {
		_, _ = fmt.Fprintln(stdout)
		_, _ = fmt.Fprintln(stdout, "Tip: for higher accuracy embeddings, download the ONNX model:")
		_, _ = fmt.Fprintln(stdout, "  pali init -download-model")
	}
	_, _ = fmt.Fprintln(stdout, "Next:")
	_, _ = fmt.Fprintf(stdout, "1) run: pali serve -config %s\n", configPath)
	_, _ = fmt.Fprintln(stdout, "2) open: http://localhost:8080/dashboard")

	return nil
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// EnsureConfig creates a default config file if one does not already exist.
func EnsureConfig(cfg string) error {
	cfg = strings.TrimSpace(cfg)
	if cfg == "" {
		cfg = "pali.yaml"
	}
	if _, err := os.Stat(cfg); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	src, err := os.ReadFile("pali.yaml.example")
	if err != nil {
		return err
	}

	dir := filepath.Dir(cfg)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
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
	defer func() {
		_ = resp.Body.Close()
	}()

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

func printRuntimeInstallHint(w io.Writer) {
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "ONNX Runtime setup hint:")
	switch runtime.GOOS {
	case "darwin":
		_, _ = fmt.Fprintln(w, "- macOS: brew install onnxruntime")
		_, _ = fmt.Fprintln(w, "- then verify /opt/homebrew/lib/libonnxruntime.dylib or set ONNXRUNTIME_SHARED_LIBRARY_PATH")
	case "windows":
		_, _ = fmt.Fprintln(w, "- Windows: download ONNX Runtime release zip and point ONNXRUNTIME_SHARED_LIBRARY_PATH to onnxruntime.dll")
		_, _ = fmt.Fprintln(w, "- also install Microsoft Visual C++ 2019 runtime")
	default:
		_, _ = fmt.Fprintln(w, "- download ONNX Runtime release archive and point ONNXRUNTIME_SHARED_LIBRARY_PATH to the shared library")
	}
	_, _ = fmt.Fprintln(w, "- release binaries: https://github.com/microsoft/onnxruntime/releases")
	_, _ = fmt.Fprintln(w, "- docs: https://onnxruntime.ai/docs/install/")
	_, _ = fmt.Fprintln(w)
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
		return fmt.Errorf("ollama is not reachable at %s: %v\ninstall: https://ollama.com/download\nthen run:\n  1) ollama serve\n  2) ollama pull %s", baseURL, err, model)
	}

	body, err := ollamaGET(ctx, client, baseURL+defaultOllamaTagsPath)
	if err != nil {
		return fmt.Errorf("failed listing ollama models at %s: %v\nrun: ollama pull %s", baseURL, err, model)
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
		return fmt.Errorf("ollama model %q is missing\nrun: ollama pull %s", model, model)
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
	defer func() {
		_ = resp.Body.Close()
	}()

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
