package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"psl/internal/pslrc"
)

func newTestClient(t *testing.T, handler http.HandlerFunc, protocol pslrc.API) (Client, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	client, err := New(&pslrc.Model{
		Name:    "test-model",
		BaseURL: srv.URL,
		APIKey:  "sk-test",
		API:     protocol,
	})
	if err != nil {
		srv.Close()
		t.Fatalf("New() error: %v", err)
	}
	return client, srv.Close
}

func decodeBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("decode body %q: %v", data, err)
	}
	return body
}

func TestAnthropicComplete(t *testing.T) {
	var got map[string]any
	var header http.Header
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, want /v1/messages", r.URL.Path)
		}
		header = r.Header.Clone()
		got = decodeBody(t, r)
		io.WriteString(w, `{"content":[{"type":"text","text":"func f() {}"}],"stop_reason":"end_turn",
			"usage":{"input_tokens":120,"output_tokens":34}}`)
	}, pslrc.APIAnthropic)
	defer done()

	out, err := client.Complete(context.Background(), Request{
		Model:  "test-model",
		System: "be terse",
		Prompt: "write f",
		Image:  &Image{MediaType: "image/png", Base64: "aGk="},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if out.Text != "func f() {}" {
		t.Errorf("Complete() = %q, want %q", out.Text, "func f() {}")
	}
	if out.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn", out.StopReason)
	}
	// Anthropic reports the two counts; the total is psl's own sum.
	if want := (Usage{InputTokens: 120, OutputTokens: 34, TotalTokens: 154}); out.Usage != want {
		t.Errorf("Usage = %+v, want %+v", out.Usage, want)
	}
	if header.Get("x-api-key") != "sk-test" {
		t.Errorf("x-api-key = %q, want the configured key", header.Get("x-api-key"))
	}
	if header.Get("anthropic-version") != anthropicVersion {
		t.Errorf("anthropic-version = %q, want %q", header.Get("anthropic-version"), anthropicVersion)
	}
	if got["system"] != "be terse" {
		t.Errorf("system = %v, want %q", got["system"], "be terse")
	}
	if got["max_tokens"] != float64(DefaultMaxTokens) {
		t.Errorf("max_tokens = %v, want %d", got["max_tokens"], DefaultMaxTokens)
	}

	content := got["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("got %d content blocks, want image + text", len(content))
	}
	image := content[0].(map[string]any)
	if image["type"] != "image" {
		t.Errorf("first block type = %v, want image", image["type"])
	}
	source := image["source"].(map[string]any)
	if source["media_type"] != "image/png" || source["data"] != "aGk=" {
		t.Errorf("image source = %v, want the supplied png", source)
	}
	if text := content[1].(map[string]any); text["text"] != "write f" {
		t.Errorf("text block = %v, want the prompt", text)
	}
}

func TestAnthropicNoImageSendsOnlyText(t *testing.T) {
	var got map[string]any
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = decodeBody(t, r)
		io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)
	}, pslrc.APIAnthropic)
	defer done()

	if _, err := client.Complete(context.Background(), Request{Model: "m", Prompt: "hi"}); err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	content := got["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("got %d content blocks, want just the text", len(content))
	}
}

func TestAnthropicTruncatedOutputIsAnError(t *testing.T) {
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"content":[{"type":"text","text":"half a fun"}],"stop_reason":"max_tokens"}`)
	}, pslrc.APIAnthropic)
	defer done()

	_, err := client.Complete(context.Background(), Request{Model: "m", Prompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "max_tokens") {
		t.Fatalf("Complete() error = %v, want a truncation error", err)
	}
}

func TestOpenAIComplete(t *testing.T) {
	var got map[string]any
	var header http.Header
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		header = r.Header.Clone()
		got = decodeBody(t, r)
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"42"}}],
			"usage":{"prompt_tokens":120,"completion_tokens":34,"total_tokens":154}}`)
	}, pslrc.APIOpenAI)
	defer done()

	out, err := client.Complete(context.Background(), Request{
		Model:     "test-model",
		System:    "be terse",
		Prompt:    "the answer",
		MaxTokens: 256,
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if out.Text != "42" {
		t.Errorf("Complete() = %q, want %q", out.Text, "42")
	}
	// OpenAI reports the total itself, so it is taken as given.
	if want := (Usage{InputTokens: 120, OutputTokens: 34, TotalTokens: 154}); out.Usage != want {
		t.Errorf("Usage = %+v, want %+v", out.Usage, want)
	}
	if header.Get("Authorization") != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want a bearer token", header.Get("Authorization"))
	}
	if got["max_completion_tokens"] != float64(256) {
		t.Errorf("max_completion_tokens = %v, want 256", got["max_completion_tokens"])
	}
	messages := got["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("got %d messages, want system + user", len(messages))
	}
	if messages[0].(map[string]any)["role"] != "system" {
		t.Errorf("first message = %v, want the system prompt", messages[0])
	}
	if messages[1].(map[string]any)["content"] != "the answer" {
		t.Errorf("user content = %v, want a plain string when there is no image", messages[1])
	}
}

func TestOpenAIImageBecomesDataURL(t *testing.T) {
	var got map[string]any
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = decodeBody(t, r)
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`)
	}, pslrc.APIOpenAI)
	defer done()

	if _, err := client.Complete(context.Background(), Request{
		Model:  "m",
		Prompt: "look",
		Image:  &Image{MediaType: "image/jpeg", Base64: "aGk="},
	}); err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	parts := got["messages"].([]any)[0].(map[string]any)["content"].([]any)
	url := parts[0].(map[string]any)["image_url"].(map[string]any)["url"]
	if url != "data:image/jpeg;base64,aGk=" {
		t.Errorf("image url = %v, want a data URL", url)
	}
}

func TestErrorResponse(t *testing.T) {
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
	}, pslrc.APIAnthropic)
	defer done()

	_, err := client.Complete(context.Background(), Request{Model: "m", Prompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("Complete() error = %v, want the API's message", err)
	}
}

func TestRetriesServerErrors(t *testing.T) {
	calls := 0
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, "boom")
			return
		}
		io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)
	}, pslrc.APIAnthropic)
	defer done()

	out, err := client.Complete(context.Background(), Request{Model: "m", Prompt: "hi"})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if out.Text != "ok" || calls != 2 {
		t.Errorf("Complete() = %q after %d calls, want %q after 2", out.Text, calls, "ok")
	}
}

func TestEndpoint(t *testing.T) {
	tests := []struct {
		model *pslrc.Model
		want  string
	}{
		{&pslrc.Model{BaseURL: "https://api.anthropic.com", API: pslrc.APIAnthropic}, "https://api.anthropic.com/v1/messages"},
		{&pslrc.Model{BaseURL: "https://api.openai.com", API: pslrc.APIOpenAI}, "https://api.openai.com/v1/chat/completions"},
		{&pslrc.Model{BaseURL: "https://api.anthropic.com"}, "https://api.anthropic.com/v1/messages"},
	}
	for _, tc := range tests {
		if got := Endpoint(tc.model); got != tc.want {
			t.Errorf("Endpoint(%q) = %q, want %q", tc.model.BaseURL, got, tc.want)
		}
	}
}

func TestNewSelectsProtocolFromBaseURL(t *testing.T) {
	anthropic, err := New(&pslrc.Model{Name: "m", BaseURL: "https://api.anthropic.com", APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := anthropic.(*anthropicClient); !ok {
		t.Errorf("New() = %T, want the Anthropic client", anthropic)
	}

	openai, err := New(&pslrc.Model{Name: "m", BaseURL: "https://api.openai.com", APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := openai.(*openAIClient); !ok {
		t.Errorf("New() = %T, want the OpenAI client", openai)
	}
}
