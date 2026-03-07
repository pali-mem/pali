//go:build onnx

package onnx

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/sugarme/tokenizer"
	ort "github.com/yalue/onnxruntime_go"
)

const (
	inputNameIDs          = "input_ids"
	inputNameMask         = "attention_mask"
	inputNameTokenTypeIDs = "token_type_ids"
)

type Embedder struct {
	ModelPath     string
	TokenizerPath string

	tokenizer  *tokenizer.Tokenizer
	session    *ort.DynamicAdvancedSession
	inputNames []string
	outputName string

	mu sync.Mutex
}

func NewEmbedder(modelPath, tokenizerPath string) (*Embedder, error) {
	if err := LoadModel(modelPath); err != nil {
		return nil, err
	}
	tk, err := loadTokenizer(tokenizerPath)
	if err != nil {
		return nil, err
	}

	if err := ensureRuntimeInitialized(); err != nil {
		return nil, err
	}

	inputs, outputs, err := ort.GetInputOutputInfo(modelPath)
	if err != nil {
		return nil, fmt.Errorf("inspect model IO: %w", err)
	}

	inputNames, err := selectInputNames(inputs)
	if err != nil {
		return nil, err
	}
	outputName, err := selectOutputName(outputs)
	if err != nil {
		return nil, err
	}

	session, err := ort.NewDynamicAdvancedSession(modelPath, inputNames, []string{outputName}, nil)
	if err != nil {
		return nil, fmt.Errorf("create onnx session: %w", err)
	}

	return &Embedder{
		ModelPath:     modelPath,
		TokenizerPath: tokenizerPath,
		tokenizer:     tk,
		session:       session,
		inputNames:    inputNames,
		outputName:    outputName,
	}, nil
}

func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e == nil || e.tokenizer == nil || e.session == nil {
		return nil, fmt.Errorf("onnx embedder is not initialized")
	}

	encoding, err := e.tokenizer.EncodeSingle(strings.TrimSpace(text), true)
	if err != nil {
		return nil, fmt.Errorf("tokenize input: %w", err)
	}

	ids := encoding.GetIds()
	if len(ids) == 0 {
		return nil, fmt.Errorf("tokenize input: empty token ids")
	}

	attentionMask := encoding.GetAttentionMask()
	if len(attentionMask) == 0 {
		attentionMask = make([]int, len(ids))
		for i := range attentionMask {
			attentionMask[i] = 1
		}
	}
	if len(attentionMask) != len(ids) {
		return nil, fmt.Errorf("tokenizer produced mismatched ids (%d) and attention mask (%d)", len(ids), len(attentionMask))
	}

	typeIDs := encoding.GetTypeIds()
	if len(typeIDs) == 0 {
		typeIDs = make([]int, len(ids))
	}
	if len(typeIDs) != len(ids) {
		return nil, fmt.Errorf("tokenizer produced mismatched ids (%d) and type ids (%d)", len(ids), len(typeIDs))
	}

	ids64 := intsToInt64(ids)
	mask64 := intsToInt64(attentionMask)
	typeIDs64 := intsToInt64(typeIDs)
	shape := ort.NewShape(1, int64(len(ids64)))

	e.mu.Lock()
	defer e.mu.Unlock()

	inputTensors := make([]ort.Value, 0, len(e.inputNames))
	for _, name := range e.inputNames {
		var src []int64
		switch name {
		case inputNameIDs:
			src = ids64
		case inputNameMask:
			src = mask64
		case inputNameTokenTypeIDs:
			src = typeIDs64
		default:
			return nil, fmt.Errorf("unsupported model input name %q", name)
		}
		t, err := ort.NewTensor(shape, src)
		if err != nil {
			destroyValues(inputTensors)
			return nil, fmt.Errorf("create input tensor %q: %w", name, err)
		}
		inputTensors = append(inputTensors, t)
	}
	defer destroyValues(inputTensors)

	outputs := []ort.Value{nil}
	if err := e.session.Run(inputTensors, outputs); err != nil {
		destroyValues(outputs)
		return nil, fmt.Errorf("run onnx session: %w", err)
	}
	defer destroyValues(outputs)

	if outputs[0] == nil {
		return nil, fmt.Errorf("onnx output %q was nil", e.outputName)
	}

	hidden, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("onnx output %q has unsupported type %T", e.outputName, outputs[0])
	}

	pooled, err := meanPoolAndNormalize(hidden.GetData(), attentionMask)
	if err != nil {
		return nil, err
	}
	return pooled, nil
}

func (e *Embedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		vec, err := e.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		out = append(out, vec)
	}
	return out, nil
}

func selectInputNames(inputs []ort.InputOutputInfo) ([]string, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("model has no inputs")
	}

	out := make([]string, 0, len(inputs))
	seen := map[string]bool{}

	for _, in := range inputs {
		name := strings.TrimSpace(in.Name)
		switch name {
		case inputNameIDs, inputNameMask, inputNameTokenTypeIDs:
			seen[name] = true
			out = append(out, name)
		default:
			return nil, fmt.Errorf("unsupported model input %q; expected BERT-style inputs", name)
		}
	}

	if !seen[inputNameIDs] {
		return nil, fmt.Errorf("model input %q is required", inputNameIDs)
	}
	if !seen[inputNameMask] {
		return nil, fmt.Errorf("model input %q is required", inputNameMask)
	}
	return out, nil
}

func selectOutputName(outputs []ort.InputOutputInfo) (string, error) {
	if len(outputs) == 0 {
		return "", fmt.Errorf("model has no outputs")
	}

	for _, out := range outputs {
		if strings.TrimSpace(out.Name) == "last_hidden_state" {
			return out.Name, nil
		}
	}
	return outputs[0].Name, nil
}

func intsToInt64(in []int) []int64 {
	out := make([]int64, len(in))
	for i, v := range in {
		out[i] = int64(v)
	}
	return out
}

func meanPoolAndNormalize(hiddenStates []float32, attentionMask []int) ([]float32, error) {
	if len(attentionMask) == 0 {
		return nil, fmt.Errorf("attention mask is empty")
	}
	if len(hiddenStates) == 0 {
		return nil, fmt.Errorf("hidden states are empty")
	}
	if len(hiddenStates)%len(attentionMask) != 0 {
		return nil, fmt.Errorf("hidden state length %d is not divisible by token count %d", len(hiddenStates), len(attentionMask))
	}

	hiddenSize := len(hiddenStates) / len(attentionMask)
	pooled := make([]float32, hiddenSize)

	var tokenCount float32
	for tokIdx, mask := range attentionMask {
		if mask <= 0 {
			continue
		}
		tokenCount++
		base := tokIdx * hiddenSize
		for i := 0; i < hiddenSize; i++ {
			pooled[i] += hiddenStates[base+i]
		}
	}
	if tokenCount == 0 {
		return nil, fmt.Errorf("attention mask contains no active tokens")
	}

	invCount := float32(1.0 / tokenCount)
	var norm float64
	for i := range pooled {
		pooled[i] *= invCount
		norm += float64(pooled[i] * pooled[i])
	}
	if norm == 0 {
		return pooled, nil
	}

	invNorm := float32(1.0 / math.Sqrt(norm))
	for i := range pooled {
		pooled[i] *= invNorm
	}
	return pooled, nil
}

func destroyValues(values []ort.Value) {
	for _, v := range values {
		if v == nil {
			continue
		}
		_ = v.Destroy()
	}
}
