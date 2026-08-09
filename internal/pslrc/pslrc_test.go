package pslrc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `default_model=claude-opus-5

[gpt-5.6]
base_url=https://api.openai.com
api_key=sk-openai

[claude-opus-5]
base_url=https://api.anthropic.com/
api_key=sk-anthropic
max_tokens=1024
`

func TestParse(t *testing.T) {
	cfg, err := Parse(sample, ".pslrc")
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if cfg.DefaultModel != "claude-opus-5" {
		t.Errorf("DefaultModel = %q, want %q", cfg.DefaultModel, "claude-opus-5")
	}
	if len(cfg.Models) != 2 {
		t.Fatalf("got %d models, want 2", len(cfg.Models))
	}

	claude := cfg.Models["claude-opus-5"]
	if claude.BaseURL != "https://api.anthropic.com" {
		t.Errorf("BaseURL = %q, trailing slash should be trimmed", claude.BaseURL)
	}
	if claude.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d, want 1024", claude.MaxTokens)
	}
	if claude.Protocol() != APIAnthropic {
		t.Errorf("Protocol() = %q, want %q", claude.Protocol(), APIAnthropic)
	}
	if claude.ID() != "claude-opus-5" {
		t.Errorf("ID() = %q, want the section name", claude.ID())
	}
	if got := cfg.Models["gpt-5.6"].Protocol(); got != APIOpenAI {
		t.Errorf("Protocol() = %q, want %q", got, APIOpenAI)
	}
}

func TestParseExtras(t *testing.T) {
	t.Setenv("PSL_TEST_KEY", "sk-from-env")
	cfg, err := Parse(`
# a comment
default_model = local   ; trailing comment

[local]
base_url = "http://localhost:11434"
api_key = ${PSL_TEST_KEY}
model = qwen3:8b
api = openai
`, ".pslrc")
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	m := cfg.Models["local"]
	if m.APIKey != "sk-from-env" {
		t.Errorf("APIKey = %q, want the expanded environment variable", m.APIKey)
	}
	if m.BaseURL != "http://localhost:11434" {
		t.Errorf("BaseURL = %q, quotes should be stripped", m.BaseURL)
	}
	if m.ID() != "qwen3:8b" {
		t.Errorf("ID() = %q, want the model= override", m.ID())
	}
	if m.Protocol() != APIOpenAI {
		t.Errorf("Protocol() = %q, want the api= override", m.Protocol())
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"key outside a section", "base_url=https://x", "must appear inside"},
		{"missing equals", "[m]\nbase_url", "expected key=value"},
		{"unknown key", "[m]\nnope=1", `unknown key "nope"`},
		{"bad max_tokens", "[m]\nmax_tokens=zero", "max_tokens must be"},
		{"bad api", "[m]\napi=grpc", "api must be"},
		{"unterminated section", "[m\n", "unterminated section"},
		{"duplicate section", "[m]\n[m]\n", "duplicate section"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.text, ".pslrc")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse() error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	cfg, err := Parse(sample, ".pslrc")
	if err != nil {
		t.Fatal(err)
	}

	m, err := cfg.Resolve("")
	if err != nil {
		t.Fatalf("Resolve(\"\") error: %v", err)
	}
	if m.Name != "claude-opus-5" {
		t.Errorf("Resolve(\"\") = %q, want default_model", m.Name)
	}

	if _, err := cfg.Resolve("gpt-5.6"); err != nil {
		t.Errorf("Resolve(%q) error: %v", "gpt-5.6", err)
	}

	_, err = cfg.Resolve("mystery-model")
	if err == nil || !strings.Contains(err.Error(), "[mystery-model]") {
		t.Errorf("Resolve() error = %v, want it to name the missing section", err)
	}
	if !strings.Contains(err.Error(), "claude-opus-5, gpt-5.6") {
		t.Errorf("Resolve() error = %v, want it to list configured models in order", err)
	}
}

func TestResolveMissingCredentials(t *testing.T) {
	cfg, err := Parse("[m]\nbase_url=https://x\napi_key=\n", ".pslrc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Resolve("m"); err == nil || !strings.Contains(err.Error(), "api_key") {
		t.Fatalf("Resolve() error = %v, want a missing api_key error", err)
	}

	cfg, err = Parse("[m]\napi_key=sk\n", ".pslrc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Resolve("m"); err == nil || !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("Resolve() error = %v, want a missing base_url error", err)
	}

	cfg, err = Parse("[m]\nbase_url=https://x\napi_key=sk\n", ".pslrc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Resolve(""); err == nil || !strings.Contains(err.Error(), "default_model") {
		t.Fatalf("Resolve() error = %v, want a missing default_model error", err)
	}
}

// noProviderEnv isolates a test from whatever API keys the developer happens to
// have exported.
func noProviderEnv(t *testing.T) {
	t.Helper()
	t.Setenv(EnvOpenAIKey, "")
	t.Setenv(EnvAnthropicKey, "")
}

func section(model string) string {
	return fmt.Sprintf("default_model=%s\n\n[%s]\nbase_url=https://example.test\napi_key=sk-file\n", model, model)
}

func TestLoadPrefersLocalFile(t *testing.T) {
	noProviderEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, Name), []byte(section("home-model")), 0o600); err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	cfg, err := Load(work)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.DefaultModel != "home-model" {
		t.Errorf("DefaultModel = %q, want the home file to be used as fallback", cfg.DefaultModel)
	}

	if err := os.WriteFile(filepath.Join(work, Name), []byte(section("local-model")), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(work)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.DefaultModel != "local-model" {
		t.Errorf("DefaultModel = %q, want the local file to win", cfg.DefaultModel)
	}
}

func TestLoadWithoutConfigOrKeys(t *testing.T) {
	noProviderEnv(t)
	t.Setenv("HOME", t.TempDir())
	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("Load() succeeded, want an error when nothing is configured")
	}
	for _, want := range []string{"no .pslrc found", EnvOpenAIKey, EnvAnthropicKey} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Load() error = %v, want it to mention %q", err, want)
		}
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	tests := []struct {
		name      string
		openai    string
		anthropic string
		wantModel string
		wantAPI   API
		wantURL   string
		wantKey   string
	}{
		{
			name: "openai key wins", openai: "sk-o", anthropic: "sk-a",
			wantModel: "gpt-5.6", wantAPI: APIOpenAI, wantURL: "https://api.openai.com", wantKey: "sk-o",
		},
		{
			name: "openai key alone", openai: "sk-o",
			wantModel: "gpt-5.6", wantAPI: APIOpenAI, wantURL: "https://api.openai.com", wantKey: "sk-o",
		},
		{
			name: "anthropic key alone", anthropic: "sk-a",
			wantModel: "claude-opus-5", wantAPI: APIAnthropic, wantURL: "https://api.anthropic.com", wantKey: "sk-a",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvOpenAIKey, tc.openai)
			t.Setenv(EnvAnthropicKey, tc.anthropic)
			t.Setenv("HOME", t.TempDir())

			cfg, err := Load(t.TempDir())
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}
			if cfg.DefaultModel != tc.wantModel {
				t.Errorf("DefaultModel = %q, want %q", cfg.DefaultModel, tc.wantModel)
			}
			m, err := cfg.Resolve("")
			if err != nil {
				t.Fatalf("Resolve(\"\") error: %v", err)
			}
			if m.Name != tc.wantModel || m.ID() != tc.wantModel {
				t.Errorf("resolved %q, want %q", m.Name, tc.wantModel)
			}
			if m.Protocol() != tc.wantAPI {
				t.Errorf("Protocol() = %q, want %q", m.Protocol(), tc.wantAPI)
			}
			if m.BaseURL != tc.wantURL {
				t.Errorf("BaseURL = %q, want %q", m.BaseURL, tc.wantURL)
			}
			if m.APIKey != tc.wantKey {
				t.Errorf("APIKey = %q, want %q", m.APIKey, tc.wantKey)
			}
			// The environment-provided model is also addressable by name.
			if _, err := cfg.Resolve(tc.wantModel); err != nil {
				t.Errorf("Resolve(%q) error: %v", tc.wantModel, err)
			}
		})
	}
}

func TestApplyEnvDoesNotOverrideConfiguredSections(t *testing.T) {
	t.Setenv(EnvOpenAIKey, "sk-env")
	t.Setenv(EnvAnthropicKey, "sk-env-a")

	cfg, err := Parse(`default_model=gpt-5.6

[gpt-5.6]
base_url=https://gateway.internal
api_key=sk-file
`, ".pslrc")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyEnv()

	m, err := cfg.Resolve("")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if m.APIKey != "sk-file" || m.BaseURL != "https://gateway.internal" {
		t.Errorf("resolved %+v, want the .pslrc section to win over the environment", m)
	}
	if cfg.DefaultModel != "gpt-5.6" {
		t.Errorf("DefaultModel = %q, want the configured default", cfg.DefaultModel)
	}
	// The other provider's key still contributes its model.
	if _, err := cfg.Resolve("claude-opus-5"); err != nil {
		t.Errorf("Resolve(claude-opus-5) error: %v", err)
	}
}

func TestApplyEnvFillsMissingKey(t *testing.T) {
	t.Setenv(EnvOpenAIKey, "sk-env")
	t.Setenv(EnvAnthropicKey, "")

	cfg, err := Parse("default_model=my-gpt\n\n[my-gpt]\nbase_url=https://api.openai.com\n", ".pslrc")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyEnv()

	m, err := cfg.Resolve("")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if m.APIKey != "sk-env" {
		t.Errorf("APIKey = %q, want a section without api_key to borrow it from the environment", m.APIKey)
	}
}

func TestApplyEnvKeepsConfiguredDefaultModel(t *testing.T) {
	t.Setenv(EnvOpenAIKey, "sk-env")
	t.Setenv(EnvAnthropicKey, "")

	cfg, err := Parse("default_model=house\n\n[house]\nbase_url=https://x\napi_key=sk\n", ".pslrc")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyEnv()
	if cfg.DefaultModel != "house" {
		t.Errorf("DefaultModel = %q, want the environment not to override default_model", cfg.DefaultModel)
	}
}

func TestResolveNamesTheEnvironmentSource(t *testing.T) {
	t.Setenv(EnvOpenAIKey, "")
	t.Setenv(EnvAnthropicKey, "sk-a")

	cfg := &Config{Models: map[string]*Model{}}
	cfg.ApplyEnv()

	_, err := cfg.Resolve("ghost")
	if err == nil {
		t.Fatal("Resolve() succeeded, want an error")
	}
	for _, want := range []string{"[ghost]", "the environment", "claude-opus-5 (from $ANTHROPIC_API_KEY)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Resolve() error = %v, want it to mention %q", err, want)
		}
	}
}
