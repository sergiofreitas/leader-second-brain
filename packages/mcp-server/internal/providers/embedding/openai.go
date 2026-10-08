// Package embedding implements embedding providers.
package embedding

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OpenAIConfig configures an OpenAI-compatible embeddings API: OpenAI
// itself, gateways and proxies like LiteLLM, or local servers like Ollama
// and LM Studio (http://localhost:11434/v1).
type OpenAIConfig struct {
	BaseURL string // e.g. https://api.openai.com/v1
	Model   string // e.g. text-embedding-3-small
	APIKey  string // sent as a Bearer token; optional for local servers
	// Dimensions asks for shorter vectors (text-embedding-3 models support
	// it); 0 uses the model's default
	Dimensions int
	// QueryPrefix and DocumentPrefix are prepended to queries and documents,
	// for models trained with them (e5: "query: " and "passage: ")
	QueryPrefix    string
	DocumentPrefix string
	Headers        map[string]string // extra headers, e.g. for a gateway
	BatchSize      int               // max inputs per request (default 64)
	Timeout        time.Duration     // per request (default 60s)
	MaxRetries     int               // retries on network errors, 429 and 5xx (default 4)
}

// OpenAI is an embedding provider for OpenAI-compatible APIs. It is safe
// for concurrent use (search queries and the indexer share it).
type OpenAI struct {
	cfg    OpenAIConfig
	client *http.Client
	sleep  func(time.Duration)

	mu   sync.Mutex
	dims int // vector size, from the config or the first response
}

// NewOpenAI validates cfg and returns the provider
func NewOpenAI(cfg OpenAIConfig) (*OpenAI, error) {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.Model = strings.TrimSpace(cfg.Model)
	if cfg.BaseURL == "" {
		return nil, errors.New("embedding: base_url is required")
	}
	if !strings.HasPrefix(cfg.BaseURL, "http://") && !strings.HasPrefix(cfg.BaseURL, "https://") {
		return nil, fmt.Errorf("embedding: base_url must start with http:// or https:// (got %q)", cfg.BaseURL)
	}
	if cfg.Model == "" {
		return nil, errors.New("embedding: model is required")
	}
	if cfg.Dimensions < 0 {
		return nil, errors.New("embedding: dimensions can't be negative")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 64
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	} else if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 4
	}
	return &OpenAI{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		sleep:  time.Sleep,
		dims:   cfg.Dimensions,
	}, nil
}

// Model identifies the vectors this provider produces: the model name, plus
// the requested dimensions (vectors of different sizes can't be compared).
// The provider isn't part of it: the same model behind OpenAI or a gateway
// produces the same vectors.
func (o *OpenAI) Model() string {
	if o.cfg.Dimensions > 0 {
		return fmt.Sprintf("%s@%d", o.cfg.Model, o.cfg.Dimensions)
	}
	return o.cfg.Model
}

// Dimensions returns the vector size (0 until known)
func (o *OpenAI) Dimensions() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.dims
}

// Embed embeds a search query
func (o *OpenAI) Embed(text string) ([]float32, error) {
	vectors, err := o.embed([]string{o.cfg.QueryPrefix + text})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

// EmbedBatch embeds documents, in requests of up to BatchSize inputs
func (o *OpenAI) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += o.cfg.BatchSize {
		end := min(start+o.cfg.BatchSize, len(texts))
		inputs := make([]string, end-start)
		for i, t := range texts[start:end] {
			inputs[i] = o.cfg.DocumentPrefix + t
		}
		vectors, err := o.embed(inputs)
		if err != nil {
			return nil, err
		}
		out = append(out, vectors...)
	}
	return out, nil
}

type embeddingRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format"`
	Dimensions     int      `json:"dimensions,omitempty"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// embed sends one request, retrying transient failures
func (o *OpenAI) embed(inputs []string) ([][]float32, error) {
	body, err := json.Marshal(embeddingRequest{
		Model: o.cfg.Model, Input: inputs, EncodingFormat: "float", Dimensions: o.cfg.Dimensions,
	})
	if err != nil {
		return nil, err
	}
	for attempt := 0; ; attempt++ {
		vectors, retryAfter, err := o.post(body, len(inputs))
		if err == nil {
			return vectors, nil
		}
		if retryAfter < 0 || attempt >= o.cfg.MaxRetries {
			return nil, err
		}
		// Exponential backoff (1s, 2s, 4s...), or what the server asked for
		wait := time.Second << uint(attempt)
		if retryAfter > 0 {
			wait = retryAfter
		}
		if wait <= 0 || wait > time.Minute {
			wait = time.Minute
		}
		o.sleep(wait)
	}
}

// post sends a request. retryAfter is negative for errors that won't go away
// by retrying, 0 to retry with backoff, or the delay the server asked for.
func (o *OpenAI) post(body []byte, n int) (vectors [][]float32, retryAfter time.Duration, err error) {
	req, err := http.NewRequest(http.MethodPost, o.cfg.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, -1, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.cfg.APIKey)
	}
	for k, v := range o.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		// Connection refused, timeout, reset...: worth retrying
		return nil, 0, fmt.Errorf("embedding request to %s: %w", o.cfg.BaseURL, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("embedding response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("embedding API returned %s: %s", resp.Status, apiErrorMessage(respBody))
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			return nil, parseRetryAfter(resp.Header.Get("Retry-After")), err
		case resp.StatusCode >= 500:
			return nil, 0, err
		default:
			return nil, -1, err
		}
	}

	var parsed embeddingResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, -1, fmt.Errorf("embedding response is not valid JSON: %w", err)
	}
	if len(parsed.Data) != n {
		return nil, -1, fmt.Errorf("embedding API returned %d vectors for %d inputs", len(parsed.Data), n)
	}
	sort.Slice(parsed.Data, func(i, j int) bool { return parsed.Data[i].Index < parsed.Data[j].Index })
	vectors = make([][]float32, n)
	for i, d := range parsed.Data {
		if d.Index != i {
			return nil, -1, fmt.Errorf("embedding API returned an unexpected index %d", d.Index)
		}
		if len(d.Embedding) == 0 {
			return nil, -1, fmt.Errorf("embedding API returned an empty vector for input %d", i)
		}
		if err := o.checkDims(len(d.Embedding)); err != nil {
			return nil, -1, err
		}
		vectors[i] = d.Embedding
	}
	return vectors, 0, nil
}

// checkDims learns the vector size from the first response and rejects
// vectors of another size (a misconfigured gateway mixing models)
func (o *OpenAI) checkDims(n int) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.dims == 0 {
		o.dims = n
	}
	if n != o.dims {
		return fmt.Errorf("embedding API returned a %d-dimension vector, expected %d", n, o.dims)
	}
	return nil
}

// apiErrorMessage extracts {"error":{"message":...}} or a short body excerpt
func apiErrorMessage(body []byte) string {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	msg := strings.TrimSpace(string(body))
	if len(msg) > 300 {
		msg = msg[:300] + "..."
	}
	if msg == "" {
		msg = "(empty body)"
	}
	return msg
}

// parseRetryAfter reads a Retry-After header in seconds (0 if absent/invalid)
func parseRetryAfter(v string) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}
