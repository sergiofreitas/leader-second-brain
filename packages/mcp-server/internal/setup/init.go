// Package setup implements `second-brain init`: writing a config file from
// one of the profiles embedded in the binary.
package setup

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/second-brain/second-brain/packages/mcp-server/configs"
)

// InitOptions configures Init
type InitOptions struct {
	Profile   string // embedded profile name (default: "default")
	Embedding string // replaces the profile's embedding: none | openai | ollama ("" keeps it)
	Path      string // where to write the config
	Force     bool   // overwrite an existing file
}

// InitResult describes the config written by Init
type InitResult struct {
	Path string
	// EnvVars are the environment variables the config reads (${NAME}),
	// with whether each is set in the current environment
	EnvVars map[string]bool
}

// EmbeddingPresets are the accepted values of InitOptions.Embedding
var EmbeddingPresets = []string{"none", "openai", "ollama"}

// embeddingPreset returns the YAML node for an embedding preset, and the
// comment that goes above it
func embeddingPreset(name string) (*yaml.Node, string, error) {
	var src, comment string
	switch name {
	case "none":
		src = "none"
		comment = "Semantic search off: search is by keyword and nothing is sent anywhere"
	case "openai":
		src = "provider: openai\nmodel: text-embedding-3-small\napi_key: \"${OPENAI_API_KEY}\"\n"
		comment = "Semantic search with OpenAI embeddings: memory text is sent to OpenAI"
	case "ollama":
		src = "provider: ollama\nmodel: bge-m3\n"
		comment = "Semantic search with a local Ollama model: nothing leaves the machine.\nRun `ollama pull bge-m3` first."
	default:
		return nil, "", fmt.Errorf("unknown embedding %q (available: %s)", name, strings.Join(EmbeddingPresets, ", "))
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		return nil, "", err
	}
	return doc.Content[0], comment, nil
}

// Init writes the config file for opts.Profile, optionally with another
// embedding provider. It refuses to overwrite a file unless opts.Force.
func Init(opts InitOptions) (*InitResult, error) {
	if opts.Profile == "" {
		opts.Profile = "default"
	}
	data, err := configs.Profile(opts.Profile)
	if err != nil {
		return nil, err
	}
	if opts.Embedding != "" {
		if data, err = replaceEmbedding(data, opts.Embedding); err != nil {
			return nil, err
		}
	}

	if !opts.Force {
		if _, err := os.Stat(opts.Path); err == nil {
			return nil, fmt.Errorf("%s already exists (use --force to overwrite it)", opts.Path)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	// The config may hold secrets (a literal api_key): keep it private
	if err := os.MkdirAll(filepath.Dir(opts.Path), 0o700); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(opts.Path, data, 0o600); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}

	vars, err := envVars(data)
	if err != nil {
		return nil, err
	}
	return &InitResult{Path: opts.Path, EnvVars: vars}, nil
}

// replaceEmbedding swaps providers.embedding in a profile, keeping the rest
// of the YAML (and its comments) as is
func replaceEmbedding(data []byte, preset string) ([]byte, error) {
	value, comment, err := embeddingPreset(preset)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse profile: %w", err)
	}
	providers := mappingValue(doc.Content[0], "providers")
	if providers == nil {
		return nil, errors.New("profile has no providers section")
	}
	replaced := false
	for i := 0; i+1 < len(providers.Content); i += 2 {
		if providers.Content[i].Value == "embedding" {
			providers.Content[i+1] = value
			// The profile's comment describes the old provider
			providers.Content[i].HeadComment = comment
			replaced = true
		}
	}
	if !replaced {
		providers.Content = append(providers.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "embedding", HeadComment: comment}, value)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// envVars returns the ${NAME} references in the config's values (not in its
// comments), with whether each variable is set
func envVars(data []byte) (map[string]bool, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	vars := map[string]bool{}
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Kind == yaml.ScalarNode {
			for _, m := range envRef.FindAllStringSubmatch(n.Value, -1) {
				_, set := os.LookupEnv(m[1])
				vars[m[1]] = set
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(&doc)
	return vars, nil
}

// SortedVars returns the variable names of an InitResult, sorted
func (r *InitResult) SortedVars() []string {
	names := make([]string, 0, len(r.EnvVars))
	for name := range r.EnvVars {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
