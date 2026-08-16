package pslrc

import (
	"strings"
	"testing"
)

func parseOrFail(t *testing.T, text string) *Config {
	t.Helper()
	cfg, err := Parse(text, ".pslrc")
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	return cfg
}

// `web_search=` is a switch first and a model name second: turning search on is
// the ordinary case, and which endpoint does the searching is not usually the
// point.
func TestWebSearchSwitch(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"on", DefaultSearchModel},
		{"true", DefaultSearchModel},
		{"yes", DefaultSearchModel},
		{"1", DefaultSearchModel},
		{"ON", DefaultSearchModel},
		{"off", ""},
		{"false", ""},
		{"no", ""},
		{"0", ""},
		{"", ""},
		{"gpt-5-search-api", DefaultSearchModel},
		{"my-local-search", "my-local-search"},
	} {
		cfg := parseOrFail(t, "[m]\nweb_search="+tc.value)
		if got := cfg.Models["m"].WebSearch; got != tc.want {
			t.Errorf("web_search=%q gave %q, want %q", tc.value, got, tc.want)
		}
	}
}

// A section that left it out is a section psl offers no tool for.
func TestWebSearchDefaultsOff(t *testing.T) {
	cfg := parseOrFail(t, "[m]\nbase_url=https://api.openai.com\napi_key=k")
	if got := cfg.Models["m"].WebSearch; got != "" {
		t.Errorf("web_search = %q with nothing written, want it off", got)
	}
}

// A misspelt switch is a typo to report, not a model nobody configured: the
// error comes from the line rather than from Resolve much later.
func TestWebSearchRejectsProse(t *testing.T) {
	_, err := Parse("[m]\nweb_search=on if needed", ".pslrc")
	if err == nil || !strings.Contains(err.Error(), "web_search must be") {
		t.Fatalf("Parse() error = %v, want the value rejected where it was written", err)
	}
}

// psl answers the calls its tool produces, so a tool arriving through params is
// one nothing would be behind.
func TestParamsMayNotSetTools(t *testing.T) {
	_, err := Parse(`[m]`+"\n"+`params={"tools": []}`, ".pslrc")
	if err == nil || !strings.Contains(err.Error(), "psl writes that field itself") {
		t.Fatalf("Parse() error = %v, want tools refused in params", err)
	}
}

// The default search model needs no section: it is OpenAI's, and the key is one
// psl can already see in the environment.
func TestResolveSearchTakesTheKeyFromTheEnvironment(t *testing.T) {
	t.Setenv(EnvOpenAIKey, "sk-env")
	cfg := parseOrFail(t, "[claude-opus-5]\nbase_url=https://api.anthropic.com\napi_key=sk-ant\nweb_search=on")

	search, err := cfg.ResolveSearch(cfg.Models["claude-opus-5"])
	if err != nil {
		t.Fatalf("ResolveSearch() error: %v", err)
	}
	if search.Name != DefaultSearchModel || search.BaseURL != DefaultSearchBaseURL {
		t.Errorf("ResolveSearch() = %s at %s, want %s at %s",
			search.Name, search.BaseURL, DefaultSearchModel, DefaultSearchBaseURL)
	}
	if search.APIKey != "sk-env" {
		t.Errorf("api key = %q, want the one in the environment", search.APIKey)
	}
}

// A .pslrc that keeps its key in the file rather than the environment is the
// ordinary case, and web_search=on has to work in it. The key is only reused
// where it would already have been sent — the same host.
func TestResolveSearchBorrowsTheKeyForTheSameHost(t *testing.T) {
	t.Setenv(EnvOpenAIKey, "")
	cfg := parseOrFail(t, "[gpt-5.6-luna]\nbase_url=https://api.openai.com\napi_key=sk-inline\nweb_search=on")

	search, err := cfg.ResolveSearch(cfg.Models["gpt-5.6-luna"])
	if err != nil {
		t.Fatalf("ResolveSearch() error: %v", err)
	}
	if search.APIKey != "sk-inline" {
		t.Errorf("api key = %q, want the one the section already reaches that host with", search.APIKey)
	}
}

// Another provider's key is not OpenAI's, so it is never quietly sent there.
// The error says what to do instead.
func TestResolveSearchWillNotSendAnotherProvidersKey(t *testing.T) {
	t.Setenv(EnvOpenAIKey, "")
	cfg := parseOrFail(t, "[claude-opus-5]\nbase_url=https://api.anthropic.com\napi_key=sk-ant\nweb_search=on")

	_, err := cfg.ResolveSearch(cfg.Models["claude-opus-5"])
	if err == nil {
		t.Fatal("ResolveSearch() succeeded, want it to refuse to send an Anthropic key to OpenAI")
	}
	if !strings.Contains(err.Error(), EnvOpenAIKey) || !strings.Contains(err.Error(), DefaultSearchModel) {
		t.Errorf("error = %v, want it to name the key to export and the section to write", err)
	}
}

// A section naming its own search model is looked up like any other, which is
// what lets a local or third-party search endpoint do the searching.
func TestResolveSearchUsesANamedSection(t *testing.T) {
	t.Setenv(EnvOpenAIKey, "")
	cfg := parseOrFail(t, `[gpt-5.6-luna]
base_url=https://api.openai.com
api_key=sk-inline
web_search=my-local-search

[my-local-search]
base_url=http://127.0.0.1:11434
api_key=ollama`)

	search, err := cfg.ResolveSearch(cfg.Models["gpt-5.6-luna"])
	if err != nil {
		t.Fatalf("ResolveSearch() error: %v", err)
	}
	if search.Name != "my-local-search" || search.BaseURL != "http://127.0.0.1:11434" {
		t.Errorf("ResolveSearch() = %s at %s, want the section that was named", search.Name, search.BaseURL)
	}
}

// Naming a section that is not there is worth saying before the slot is paid
// for, and worth saying in terms of the section that asked.
func TestResolveSearchReportsAMissingSection(t *testing.T) {
	cfg := parseOrFail(t, "[m]\nbase_url=https://api.openai.com\napi_key=k\nweb_search=nowhere")

	_, err := cfg.ResolveSearch(cfg.Models["m"])
	if err == nil || !strings.Contains(err.Error(), `web_search for model "m"`) {
		t.Fatalf("ResolveSearch() error = %v, want it to name the section that asked", err)
	}
}

// Search off is the default, and nothing is resolved for it.
func TestResolveSearchIsNilWhenOff(t *testing.T) {
	cfg := parseOrFail(t, "[m]\nbase_url=https://api.openai.com\napi_key=k")

	search, err := cfg.ResolveSearch(cfg.Models["m"])
	if err != nil {
		t.Fatalf("ResolveSearch() error: %v", err)
	}
	if search != nil {
		t.Errorf("ResolveSearch() = %+v, want nothing for a section that left search off", search)
	}
}

// A section written for the search model itself wins over the one psl would
// have made up, the same as anywhere else in .pslrc.
func TestConfiguredSearchSectionWins(t *testing.T) {
	t.Setenv(EnvOpenAIKey, "sk-env")
	cfg := parseOrFail(t, `[m]
base_url=https://api.openai.com
api_key=k
web_search=on

[`+DefaultSearchModel+`]
base_url=https://proxy.internal
api_key=sk-proxy`)

	search, err := cfg.ResolveSearch(cfg.Models["m"])
	if err != nil {
		t.Fatalf("ResolveSearch() error: %v", err)
	}
	if search.BaseURL != "https://proxy.internal" || search.APIKey != "sk-proxy" {
		t.Errorf("ResolveSearch() = %s with %q, want the section that was written", search.BaseURL, search.APIKey)
	}
}
