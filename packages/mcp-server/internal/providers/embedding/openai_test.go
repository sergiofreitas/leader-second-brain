package embedding

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAPI is an OpenAI-compatible /embeddings endpoint. Each input is
// embedded as [len(input), index, 1]; responses list vectors in reverse
// order, to check the client reorders them by index.
type fakeAPI struct {
	mu       sync.Mutex
	requests []embeddingRequest
	headers  []http.Header
	// fail returns a status (and Retry-After) for the n-th request, or 0
	fail func(n int) (status int, retryAfter string)
	dims int // if set, every vector has this many dimensions
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/embeddings" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var req embeddingRequest
	json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	n := len(f.requests)
	f.requests = append(f.requests, req)
	f.headers = append(f.headers, r.Header.Clone())
	f.mu.Unlock()

	if f.fail != nil {
		if status, retryAfter := f.fail(n); status != 0 {
			if retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"error":{"message":"fake error %d"}}`, status)
			return
		}
	}

	type item struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	}
	data := make([]item, len(req.Input))
	for i, in := range req.Input {
		vec := []float32{float32(len(in)), float32(i), 1}
		if f.dims > 0 {
			vec = make([]float32, f.dims)
			vec[0] = float32(len(in))
		}
		data[len(req.Input)-1-i] = item{Index: i, Embedding: vec}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"object": "list", "data": data})
}

func newProvider(t *testing.T, api http.Handler, cfg OpenAIConfig) (*OpenAI, *[]time.Duration) {
	t.Helper()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	cfg.BaseURL = srv.URL + "/v1/"
	if cfg.Model == "" {
		cfg.Model = "text-embedding-3-small"
	}
	p, err := NewOpenAI(cfg)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	var slept []time.Duration
	p.sleep = func(d time.Duration) { slept = append(slept, d) }
	return p, &slept
}

func TestEmbedBatch(t *testing.T) {
	api := &fakeAPI{}
	p, _ := newProvider(t, api, OpenAIConfig{
		APIKey: "sk-test", BatchSize: 2, Dimensions: 3,
		DocumentPrefix: "passage: ", Headers: map[string]string{"X-Team": "lideranca"},
	})

	vectors, err := p.EmbedBatch([]string{"a", "bb", "ccc", "dddd", "eeeee"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(api.requests) != 3 {
		t.Fatalf("%d requests, want 3 batches of up to 2", len(api.requests))
	}
	// Vectors come back in input order, although the API listed them reversed
	for i, v := range vectors {
		if want := float32(len("passage: ") + i + 1); v[0] != want {
			t.Errorf("vector %d = %v, want first value %v", i, v, want)
		}
	}
	req := api.requests[0]
	if req.Model != "text-embedding-3-small" || req.Dimensions != 3 || req.EncodingFormat != "float" ||
		fmt.Sprint(req.Input) != "[passage: a passage: bb]" {
		t.Errorf("request = %+v", req)
	}
	h := api.headers[0]
	if h.Get("Authorization") != "Bearer sk-test" || h.Get("X-Team") != "lideranca" || h.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", h)
	}
	if p.Dimensions() != 3 || p.Model() != "text-embedding-3-small@3" {
		t.Errorf("dimensions %d, model %q", p.Dimensions(), p.Model())
	}
}

func TestEmbedQueryUsesQueryPrefix(t *testing.T) {
	api := &fakeAPI{}
	p, _ := newProvider(t, api, OpenAIConfig{Model: "multilingual-e5-small", QueryPrefix: "query: "})
	if _, err := p.Embed("delegação"); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if got := api.requests[0].Input; len(got) != 1 || got[0] != "query: delegação" {
		t.Errorf("input = %q", got)
	}
	if api.headers[0].Get("Authorization") != "" {
		t.Error("sent an Authorization header without an API key (local servers need none)")
	}
	if p.Model() != "multilingual-e5-small" {
		t.Errorf("model = %q", p.Model())
	}
}

func TestRetries(t *testing.T) {
	t.Run("429 honors Retry-After, then succeeds", func(t *testing.T) {
		api := &fakeAPI{fail: func(n int) (int, string) {
			if n == 0 {
				return http.StatusTooManyRequests, "7"
			}
			return 0, ""
		}}
		p, slept := newProvider(t, api, OpenAIConfig{})
		if _, err := p.Embed("x"); err != nil {
			t.Fatalf("Embed: %v", err)
		}
		if fmt.Sprint(*slept) != "[7s]" {
			t.Errorf("slept %v, want [7s]", *slept)
		}
	})

	t.Run("5xx backs off exponentially and gives up", func(t *testing.T) {
		api := &fakeAPI{fail: func(int) (int, string) { return http.StatusBadGateway, "" }}
		p, slept := newProvider(t, api, OpenAIConfig{MaxRetries: 3})
		_, err := p.Embed("x")
		if err == nil || !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "fake error 502") {
			t.Fatalf("error = %v, want the 502 and the API's message", err)
		}
		if len(api.requests) != 4 || fmt.Sprint(*slept) != "[1s 2s 4s]" {
			t.Errorf("%d requests, slept %v; want 4 requests and [1s 2s 4s]", len(api.requests), *slept)
		}
	})

	t.Run("4xx is not retried", func(t *testing.T) {
		api := &fakeAPI{fail: func(int) (int, string) { return http.StatusUnauthorized, "" }}
		p, slept := newProvider(t, api, OpenAIConfig{APIKey: "sk-secret"})
		_, err := p.Embed("x")
		if err == nil || !strings.Contains(err.Error(), "401") || len(api.requests) != 1 || len(*slept) != 0 {
			t.Errorf("err %v, %d requests, slept %v; want one failed request", err, len(api.requests), *slept)
		}
		if strings.Contains(err.Error(), "sk-secret") {
			t.Error("the API key leaked into the error message")
		}
	})

	t.Run("connection errors are retried", func(t *testing.T) {
		p, err := NewOpenAI(OpenAIConfig{BaseURL: "http://127.0.0.1:1/v1", Model: "m", MaxRetries: 2, Timeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		var slept []time.Duration
		p.sleep = func(d time.Duration) { slept = append(slept, d) }
		if _, err := p.Embed("x"); err == nil {
			t.Fatal("Embed succeeded against a closed port")
		}
		if len(slept) != 2 {
			t.Errorf("slept %v, want 2 retries", slept)
		}
	})
}

func TestInvalidResponses(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"wrong count": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"data":[]}`)
		},
		"not JSON": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `<html>gateway login</html>`)
		},
		"empty vector": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"data":[{"index":0,"embedding":[]}]}`)
		},
	}
	for name, h := range cases {
		p, slept := newProvider(t, h, OpenAIConfig{})
		if _, err := p.Embed("x"); err == nil {
			t.Errorf("%s: Embed succeeded", name)
		}
		if len(*slept) != 0 {
			t.Errorf("%s: retried a response that won't change", name)
		}
	}

	// A vector of a different size than the previous ones is rejected
	api := &fakeAPI{dims: 4}
	p, _ := newProvider(t, api, OpenAIConfig{})
	if _, err := p.Embed("x"); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	api.dims = 5
	if _, err := p.Embed("x"); err == nil || !strings.Contains(err.Error(), "expected 4") {
		t.Errorf("mixed dimensions: err = %v", err)
	}
}

func TestConfigValidation(t *testing.T) {
	cases := []struct {
		cfg  OpenAIConfig
		want string
	}{
		{OpenAIConfig{Model: "m"}, "base_url is required"},
		{OpenAIConfig{BaseURL: "api.openai.com/v1", Model: "m"}, "must start with http"},
		{OpenAIConfig{BaseURL: "https://api.openai.com/v1"}, "model is required"},
		{OpenAIConfig{BaseURL: "https://api.openai.com/v1", Model: "m", Dimensions: -1}, "negative"},
	}
	for _, c := range cases {
		if _, err := NewOpenAI(c.cfg); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("NewOpenAI(%+v) = %v, want %q", c.cfg, err, c.want)
		}
	}
}
