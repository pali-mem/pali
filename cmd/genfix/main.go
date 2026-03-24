// Command genfix generates Ollama-backed fixture data for benchmarks.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var ollamaHTTPClient = &http.Client{Timeout: 120 * time.Second}

// Fixture matches Pali's store API request body.
type Fixture struct {
	TenantID string   `json:"tenant_id"`
	Content  string   `json:"content"`
	Tags     []string `json:"tags"`
	Tier     string   `json:"tier"`
}

// ollamaRequest is the body sent to POST /api/generate.
type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
	Seed   *int   `json:"seed,omitempty"`
}

// ollamaResponse is the relevant subset of Ollama's response.
type ollamaResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

type category struct {
	name   string
	tags   []string
	tier   string
	weight int // relative weight for distribution
}

type lengthProfile struct {
	label     string
	sentences int
}

type generationResult struct {
	idx      int
	fixture  Fixture
	category string
	err      error
}

var categories = []category{
	{name: "preferences", tags: []string{"preferences"}, tier: "semantic", weight: 30},
	{name: "profile fact", tags: []string{"profile"}, tier: "semantic", weight: 20},
	{name: "episodic event", tags: []string{"event"}, tier: "episodic", weight: 35},
	{name: "transient context", tags: []string{"context"}, tier: "episodic", weight: 15},
}

var lengthProfiles = []lengthProfile{
	{label: "short (1 sentence)", sentences: 1},
	{label: "medium (2-3 sentences)", sentences: 2},
	{label: "long (4-5 sentences)", sentences: 4},
}

func main() {
	model := flag.String("model", "phi4-mini", "Ollama model to use for generation")
	count := flag.Int("count", 100, "Number of memories to generate")
	tenants := flag.Int("tenants", 10, "Number of synthetic tenants")
	out := flag.String("out", "test/benchmarks/generated/memories.generated.json", "Output file path")
	seed := flag.Int("seed", 0, "Random seed (0 = time-based)")
	ollamaURL := flag.String("ollama", "http://localhost:11434", "Ollama base URL")
	parallel := flag.Int("parallel", max(2, runtime.NumCPU()/2), "Parallel Ollama generate requests")
	flag.Parse()

	if *count <= 0 {
		fatalf("--count must be > 0\n")
	}
	if *tenants <= 0 {
		fatalf("--tenants must be > 0\n")
	}
	if *parallel <= 0 {
		fatalf("--parallel must be > 0\n")
	}

	actualSeed := int64(*seed)
	if actualSeed == 0 {
		actualSeed = time.Now().UnixNano()
	}

	if err := pingOllama(*ollamaURL); err != nil {
		fatalf("cannot reach Ollama at %s: %v\nMake sure ollama is running (ollama serve)\n", *ollamaURL, err)
	}

	totalWeight := 0
	for _, c := range categories {
		totalWeight += c.weight
	}

	fmt.Printf("==> Pali fixture generator\n")
	fmt.Printf("    mode         : ollama\n")
	fmt.Printf("    model        : %s\n", *model)
	fmt.Printf("    count        : %d\n", *count)
	fmt.Printf("    tenants      : %d\n", *tenants)
	fmt.Printf("    seed         : %d\n", actualSeed)
	fmt.Printf("    parallel     : %d\n", *parallel)
	fmt.Printf("    out          : %s\n\n", *out)

	writer, err := newFixtureWriter(*out)
	if err != nil {
		fatalf("failed opening output: %v\n", err)
	}
	defer func() {
		if closeErr := writer.Close(); closeErr != nil {
			fatalf("failed finalizing output: %v\n", closeErr)
		}
	}()

	start := time.Now()
	progressStep := chooseProgressStep(*count, isTerminal())
	catCounts, err := generateAndWriteFixtures(*count, *parallel, actualSeed, *tenants, totalWeight, *ollamaURL, *model, writer, start, progressStep)
	if err != nil {
		fatalf("%v\n", err)
	}

	fmt.Printf("\n")
	elapsed := time.Since(start)
	rate := float64(*count) / elapsed.Seconds()

	fmt.Printf("==> Done in %s\n\n", elapsed.Round(time.Millisecond))
	fmt.Printf("Output      : %s\n", *out)
	fmt.Printf("Total       : %d memories across %d tenants\n", *count, *tenants)
	fmt.Printf("Seed        : %d\n", actualSeed)
	fmt.Printf("Parallel    : %d\n", *parallel)
	fmt.Printf("Rate        : %.1f items/s\n\n", rate)

	fmt.Printf("Category breakdown:\n")
	for _, c := range categories {
		fmt.Printf("  %-20s %d\n", c.name, catCounts[c.name])
	}

	fmt.Printf("\nTo reproduce: go run ./cmd/genfix --model %s --count %d --tenants %d --seed %d --parallel %d --out %s\n",
		*model, *count, *tenants, actualSeed, *parallel, *out)
}

func generateAndWriteFixtures(count, parallel int, seedBase int64, tenants, totalWeight int, ollamaURL, model string, writer *fixtureWriter, start time.Time, progressStep int) (map[string]int, error) {
	jobs := make(chan int)
	results := make(chan generationResult, parallel*2)

	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				fixture, category, err := generateFixtureViaOllama(seedBase, idx, tenants, totalWeight, ollamaURL, model)
				results <- generationResult{idx: idx, fixture: fixture, category: category, err: err}
			}
		}()
	}

	go func() {
		for i := 0; i < count; i++ {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	pending := make(map[int]generationResult, parallel*2)
	catCounts := make(map[string]int)
	next := 0
	written := 0
	var firstErr error

	for res := range results {
		if res.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("generation failed for item %d: %w", res.idx+1, res.err)
			}
			continue
		}
		pending[res.idx] = res

		for {
			item, ok := pending[next]
			if !ok {
				break
			}
			if err := writer.Write(item.fixture); err != nil {
				return nil, fmt.Errorf("write output failed at item %d: %w", next+1, err)
			}
			catCounts[item.category]++
			written++
			printProgress(written, count, start, progressStep)
			delete(pending, next)
			next++
		}
	}

	if firstErr != nil {
		return nil, firstErr
	}
	if written != count {
		return nil, fmt.Errorf("generation incomplete: wrote %d/%d items", written, count)
	}
	return catCounts, nil
}

func chooseProgressStep(count int, interactive bool) int {
	if count <= 0 {
		return 1
	}
	if !interactive {
		return count
	}
	step := count / 200
	if step < 1 {
		step = 1
	}
	return step
}

func isTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func printProgress(done, total int, start time.Time, step int) {
	if done < total && done%step != 0 {
		return
	}
	elapsed := time.Since(start)
	seconds := elapsed.Seconds()
	if seconds <= 0 {
		seconds = 0.001
	}
	rate := float64(done) / seconds
	remainingSecs := 0.0
	if rate > 0 {
		remainingSecs = float64(total-done) / rate
	}
	remaining := time.Duration(remainingSecs * float64(time.Second))
	fmt.Printf("\r  [%d/%d] %.1f items/s  ~%s remaining   ",
		done, total, rate, remaining.Round(time.Second))
	if done == total {
		fmt.Printf("\n")
	}
}

func generateFixtureViaOllama(seedBase int64, idx, tenants, totalWeight int, ollamaURL, model string) (Fixture, string, error) {
	// Per-index deterministic RNG keeps outputs reproducible regardless of worker scheduling.
	rng := rand.New(rand.NewSource(seedBase + int64(idx)*7919 + 17))
	cat := pickCategory(rng, totalWeight)
	length := lengthProfiles[rng.Intn(len(lengthProfiles))]
	tenantID := fmt.Sprintf("bench_tenant_%03d", rng.Intn(tenants)+1)
	prompt := buildPrompt(cat.name, length.label)

	seedValue := int(seedBase + int64(idx))
	content, err := generate(ollamaURL, model, prompt, &seedValue)
	if err != nil {
		return Fixture{}, "", err
	}

	return Fixture{
		TenantID: tenantID,
		Content:  cleanContent(content),
		Tags:     cat.tags,
		Tier:     cat.tier,
	}, cat.name, nil
}

func buildPrompt(catName, length string) string {
	return fmt.Sprintf(`Generate a single realistic memory for an AI assistant to store about a user.
Category: %s
Length: %s
Return only the memory string. No preamble, no explanation, no quotes.`, catName, length)
}

func pickCategory(rng *rand.Rand, totalWeight int) category {
	n := rng.Intn(totalWeight)
	for _, c := range categories {
		n -= c.weight
		if n < 0 {
			return c
		}
	}
	return categories[len(categories)-1]
}

func cleanContent(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func pingOllama(baseURL string) error {
	resp, err := ollamaHTTPClient.Get(baseURL)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	return nil
}

func generate(baseURL, model, prompt string, seed *int) (string, error) {
	req := ollamaRequest{
		Model:  model,
		Prompt: prompt,
		Stream: false,
		Seed:   seed,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	resp, err := ollamaHTTPClient.Post(baseURL+"/api/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var olResp ollamaResponse
	if err := json.Unmarshal(raw, &olResp); err != nil {
		return "", fmt.Errorf("bad response: %w — body: %s", err, string(raw))
	}
	if olResp.Error != "" {
		return "", fmt.Errorf("ollama error: %s", olResp.Error)
	}
	return olResp.Response, nil
}

type fixtureWriter struct {
	file      *os.File
	wroteItem bool
	closed    bool
}

func newFixtureWriter(path string) (*fixtureWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if _, err := f.WriteString("[\n"); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &fixtureWriter{file: f}, nil
}

func (w *fixtureWriter) Write(f Fixture) error {
	if w.closed {
		return errors.New("writer already closed")
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if w.wroteItem {
		if _, err := w.file.WriteString(",\n"); err != nil {
			return err
		}
	}
	if _, err := w.file.WriteString("  "); err != nil {
		return err
	}
	if _, err := w.file.Write(raw); err != nil {
		return err
	}
	w.wroteItem = true
	return nil
}

func (w *fixtureWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.wroteItem {
		if _, err := w.file.WriteString("\n"); err != nil {
			_ = w.file.Close()
			return err
		}
	}
	if _, err := w.file.WriteString("]\n"); err != nil {
		_ = w.file.Close()
		return err
	}
	return w.file.Close()
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format, args...)
	os.Exit(1)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
