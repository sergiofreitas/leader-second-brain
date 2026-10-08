package providers

// Stub implementations for the MVP scaffold.
// These will be replaced by real provider implementations
// (Whisper, Tesseract, Ollama, sentence-transformers, OpenAI, Anthropic, Toqan).

// StubEmbedding returns a zero vector of the configured dimensions.
// Replace with sentence-transformers (local) or OpenAI embeddings (cloud).
type StubEmbedding struct {
	Dim int
}

func (s *StubEmbedding) Embed(text string) ([]float32, error) {
	dim := s.Dim
	if dim == 0 {
		dim = 384 // default for all-MiniLM-L6-v2
	}
	vec := make([]float32, dim)
	return vec, nil
}

func (s *StubEmbedding) EmbedBatch(texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range texts {
		v, _ := s.Embed(texts[i])
		result[i] = v
	}
	return result, nil
}

func (s *StubEmbedding) Dimensions() int {
	if s.Dim == 0 {
		return 384
	}
	return s.Dim
}

// StubLLM returns a minimal entity extraction without calling any model.
// Replace with Ollama (local) or OpenAI/Anthropic API (cloud) or Toqan (proxy).
type StubLLM struct{}

func (s *StubLLM) Complete(prompt string, systemPrompt string) (string, error) {
	return "[stub: LLM not yet implemented — configure a real provider]", nil
}

func (s *StubLLM) ExtractEntities(text string, config ExtractionConfig) (*EntityExtraction, error) {
	// Return a minimal extraction — real implementation will call the LLM
	// with a prompt that includes the feedback categories from config
	return &EntityExtraction{
		MemoryType:  "observation",
		Summary:     text,
		AboutPerson: "",
		Persons:     []ExtractedPerson{},
		Topics:      []string{},
		Tasks:       []ExtractedTask{},
		Relationships: []ExtractedRel{},
	}, nil
}
