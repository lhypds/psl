// Package pslrc reads the .pslrc configuration file.
//
//	default_model=claude-opus-5
//
//	[gpt-5.6]
//	base_url=https://api.openai.com
//	api_key=<your_openai_api_key>
//
//	[claude-opus-5]
//	base_url=https://api.anthropic.com
//	api_key=<your_anthropic_api_key>
//
// A section name is the model name written in a slot: `[claude-opus-5]` is what
// makes `:: claude-opus-5> xxx ::` resolve. Values may reference environment
// variables as ${VAR}.
//
// What .pslrc leaves unconfigured is filled in from the provider API keys in the
// environment, so psl runs with no configuration file at all. See ApplyEnv.
package pslrc

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Name is the configuration file's fixed name.
const Name = ".pslrc"

// Environment variables holding a provider API key.
const (
	EnvOpenAIKey    = "OPENAI_API_KEY"
	EnvAnthropicKey = "ANTHROPIC_API_KEY"
)

// providers describes the model psl falls back to for each API key it finds in
// the environment, in preference order: OpenAI first, then Anthropic. Each
// entry names that provider's frontier model — update it as they release.
var providers = []struct {
	Env     string
	Model   string
	BaseURL string
}{
	{EnvOpenAIKey, "gpt-5.6", "https://api.openai.com"},
	{EnvAnthropicKey, "claude-opus-5", "https://api.anthropic.com"},
}

// Model is one `[section]` of the configuration.
type Model struct {
	Name      string // section name, and the default model id sent to the API
	BaseURL   string
	APIKey    string
	ModelID   string // `model=` override when the API's id differs from the section name
	MaxTokens int    // `max_tokens=`, 0 means the package default
	Origin    string // .pslrc path, or "$VAR" when the environment supplied it
}

// Config is a parsed .pslrc.
type Config struct {
	Path         string
	DefaultModel string
	Models       map[string]*Model
}

// Load reads the .pslrc that applies to dir: the one in dir if present,
// otherwise the one in the user's home directory. Whatever it leaves
// unconfigured is filled in from the environment, so a missing .pslrc is not an
// error as long as a provider API key is exported.
func Load(dir string) (*Config, error) {
	var cfg *Config
	var tried []string
	for _, candidate := range searchPath(dir) {
		tried = append(tried, candidate)
		data, err := os.ReadFile(candidate)
		if err == nil {
			if cfg, err = Parse(string(data), candidate); err != nil {
				return nil, err
			}
			break
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read %s: %w", candidate, err)
		}
	}
	if cfg == nil {
		cfg = &Config{Models: map[string]*Model{}}
	}
	cfg.ApplyEnv()

	if len(cfg.Models) == 0 {
		if cfg.Path == "" {
			return nil, fmt.Errorf("no %s found (looked in %s), and neither %s nor %s is set in the environment",
				Name, strings.Join(tried, ", "), EnvOpenAIKey, EnvAnthropicKey)
		}
		return nil, fmt.Errorf("%s configures no models, and neither %s nor %s is set in the environment",
			cfg.Path, EnvOpenAIKey, EnvAnthropicKey)
	}
	return cfg, nil
}

// ApplyEnv fills in what .pslrc did not configure from the provider API keys in
// the environment: OPENAI_API_KEY first, then ANTHROPIC_API_KEY. Each key
// contributes that provider's frontier model, and the first key found becomes
// the default model unless .pslrc named one. Sections written in .pslrc always
// win; the environment only supplies what is missing, including the api_key of
// a section that has none.
func (c *Config) ApplyEnv() {
	if c.Models == nil {
		c.Models = map[string]*Model{}
	}
	for _, p := range providers {
		key := os.Getenv(p.Env)
		if key == "" {
			continue
		}
		if existing, ok := c.Models[p.Model]; ok {
			if existing.APIKey == "" {
				existing.APIKey = key
			}
		} else {
			c.Models[p.Model] = &Model{
				Name:    p.Model,
				BaseURL: p.BaseURL,
				APIKey:  key,
				Origin:  "$" + p.Env,
			}
		}
		if c.DefaultModel == "" {
			c.DefaultModel = p.Model
		}
	}
	// A section that never got a key can still borrow the one belonging to the
	// provider it talks to.
	for _, m := range c.Models {
		if m.APIKey == "" && m.BaseURL != "" {
			m.APIKey = os.Getenv(envKeyFor(m.BaseURL))
		}
	}
}

// envKeyFor names the environment variable holding the key an endpoint takes.
// Anything unrecognized is assumed to want an OpenAI key, since that is the
// protocol every endpoint speaks.
func envKeyFor(baseURL string) string {
	if strings.Contains(strings.ToLower(baseURL), "anthropic") {
		return EnvAnthropicKey
	}
	return EnvOpenAIKey
}

func searchPath(dir string) []string {
	paths := []string{filepath.Join(dir, Name)}
	if home, err := os.UserHomeDir(); err == nil {
		if p := filepath.Join(home, Name); p != paths[0] {
			paths = append(paths, p)
		}
	}
	return paths
}

// Parse reads configuration text. path is used for error messages only.
func Parse(text, path string) (*Config, error) {
	cfg := &Config{Path: path, Models: map[string]*Model{}}
	var current *Model

	for n, line := range strings.Split(text, "\n") {
		lineNo := n + 1
		line = strings.TrimSpace(stripComment(line))
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return nil, fmt.Errorf("%s:%d: unterminated section header", path, lineNo)
			}
			name := strings.TrimSpace(line[1 : len(line)-1])
			if name == "" {
				return nil, fmt.Errorf("%s:%d: empty section name", path, lineNo)
			}
			if _, dup := cfg.Models[name]; dup {
				return nil, fmt.Errorf("%s:%d: duplicate section [%s]", path, lineNo, name)
			}
			current = &Model{Name: name, Origin: path}
			cfg.Models[name] = current
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected key=value, got %q", path, lineNo, line)
		}
		key = strings.TrimSpace(key)
		value = expandEnv(unquote(strings.TrimSpace(value)))

		if current == nil {
			if key != "default_model" {
				return nil, fmt.Errorf("%s:%d: %q must appear inside a [model] section", path, lineNo, key)
			}
			cfg.DefaultModel = value
			continue
		}
		if err := current.set(key, value); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
	}
	return cfg, nil
}

func (m *Model) set(key, value string) error {
	switch key {
	case "base_url":
		m.BaseURL = strings.TrimRight(value, "/")
	case "api_key":
		m.APIKey = value
	case "model":
		m.ModelID = value
	case "api":
		// Accepted and ignored. It used to pick a wire protocol; there is only
		// one now, so an existing .pslrc keeps working rather than failing on a
		// key that no longer decides anything.
	case "max_tokens":
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return fmt.Errorf("max_tokens must be a positive integer, got %q", value)
		}
		m.MaxTokens = n
	case "default_model":
		return fmt.Errorf("default_model must appear before any [model] section")
	default:
		return fmt.Errorf("unknown key %q", key)
	}
	return nil
}

// Resolve returns the configuration for the model a slot asked for. An empty
// name selects default_model.
func (c *Config) Resolve(name string) (*Model, error) {
	if name == "" {
		if c.DefaultModel == "" {
			return nil, fmt.Errorf("no model named in the slot and no default_model set in %s (configured: %s)",
				c.source(), c.modelNames())
		}
		name = c.DefaultModel
	}
	m, ok := c.Models[name]
	if !ok {
		return nil, fmt.Errorf("model %q has no [%s] section in %s (configured: %s)",
			name, name, c.source(), c.modelNames())
	}
	if m.APIKey == "" {
		return nil, fmt.Errorf("model %q has no api_key in %s; set it there or export %s",
			name, m.Origin, envKeyFor(m.BaseURL))
	}
	if m.BaseURL == "" {
		return nil, fmt.Errorf("model %q has no base_url in %s", name, m.Origin)
	}
	return m, nil
}

// source names where the configuration came from, for error messages.
func (c *Config) source() string {
	if c.Path != "" {
		return c.Path
	}
	return "the environment"
}

func (c *Config) modelNames() string {
	if len(c.Models) == 0 {
		return "none"
	}
	names := make([]string, 0, len(c.Models))
	for name, m := range c.Models {
		if strings.HasPrefix(m.Origin, "$") {
			name += " (from " + m.Origin + ")"
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// ID is the model identifier sent to the API.
func (m *Model) ID() string {
	if m.ModelID != "" {
		return m.ModelID
	}
	return m.Name
}

func stripComment(line string) string {
	for i, r := range line {
		if r == '#' || r == ';' {
			return line[:i]
		}
	}
	return line
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		return v[1 : len(v)-1]
	}
	return v
}

// expandEnv resolves ${VAR} references, leaving unset variables empty so that
// Resolve reports the missing key rather than sending a literal "${VAR}".
func expandEnv(v string) string {
	var b strings.Builder
	for {
		i := strings.Index(v, "${")
		if i < 0 {
			b.WriteString(v)
			return b.String()
		}
		j := strings.IndexByte(v[i:], '}')
		if j < 0 {
			b.WriteString(v)
			return b.String()
		}
		b.WriteString(v[:i])
		b.WriteString(os.Getenv(v[i+2 : i+j]))
		v = v[i+j+1:]
	}
}
