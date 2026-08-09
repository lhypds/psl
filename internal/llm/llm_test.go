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

func newTestClient(t *testing.T, handler http.HandlerFunc) (Client, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	return New(&pslrc.Model{Name: "test-model", BaseURL: srv.URL, APIKey: "sk-test"}), srv.Close
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

func TestComplete(t *testing.T) {
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
	})
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
	// The endpoint reports the total itself, so it is taken as given.
	if want := (Usage{InputTokens: 120, OutputTokens: 34, TotalTokens: 154}); out.Usage != want {
		t.Errorf("Usage = %+v, want %+v", out.Usage, want)
	}
	// The key is a bearer token whatever the provider — that is what lets a
	// model be configured by base URL alone.
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

func TestDefaultMaxTokens(t *testing.T) {
	var got map[string]any
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = decodeBody(t, r)
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`)
	})
	defer done()

	if _, err := client.Complete(context.Background(), Request{Model: "m", Prompt: "hi"}); err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if got["max_completion_tokens"] != float64(DefaultMaxTokens) {
		t.Errorf("max_completion_tokens = %v, want %d", got["max_completion_tokens"], DefaultMaxTokens)
	}
}

func TestImageBecomesDataURL(t *testing.T) {
	var got map[string]any
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = decodeBody(t, r)
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`)
	})
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

func TestTruncatedOutputIsAnError(t *testing.T) {
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"finish_reason":"length","message":{"content":"half a fun"}}]}`)
	})
	defer done()

	_, err := client.Complete(context.Background(), Request{Model: "m", Prompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "token limit") {
		t.Fatalf("Complete() error = %v, want a truncation error", err)
	}
}

// What the log records has to be what the endpoint received, or a logged
// request cannot explain the reply it got back.
func TestBodyMatchesWhatIsSent(t *testing.T) {
	req := Request{Model: "test-model", System: "be terse", Prompt: "write f", MaxTokens: 256}

	var sent map[string]any
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		sent = decodeBody(t, r)
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`)
	})
	defer done()

	if _, err := client.Complete(context.Background(), req); err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	logged, err := Body(req)
	if err != nil {
		t.Fatalf("Body() error: %v", err)
	}
	if got, want := canonical(t, string(logged)), canonical(t, sent); got != want {
		t.Errorf("Body() = %s, want %s", got, want)
	}
}

// An image is the one thing a logged body does not reproduce: its payload is
// worth nothing on a log line, its size is worth something.
func TestBodyElidesTheImage(t *testing.T) {
	req := Request{Model: "m", Prompt: "look", Image: &Image{MediaType: "image/png", Base64: "aGVsbG8="}}
	body, err := Body(req)
	if err != nil {
		t.Fatalf("Body() error: %v", err)
	}
	if strings.Contains(string(body), "aGVsbG8=") {
		t.Errorf("Body() = %s, want the image payload left out", body)
	}
	if !strings.Contains(string(body), "…5 bytes elided…") {
		t.Errorf("Body() = %s, want the image's decoded size in its place", body)
	}
	if !strings.Contains(string(body), "image/png") {
		t.Errorf("Body() = %s, want the media type kept", body)
	}
	// Eliding must not disturb the request the caller still holds.
	if req.Image.Base64 != "aGVsbG8=" {
		t.Errorf("req.Image.Base64 = %q, want the caller's request untouched", req.Image.Base64)
	}
}

// canonical renders a value as JSON with map keys sorted, so two encodings of
// the same body compare equal whatever order their fields were written in.
func canonical(t *testing.T, v any) string {
	t.Helper()
	if s, ok := v.(string); ok {
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			t.Fatalf("decode %q: %v", s, err)
		}
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode %v: %v", v, err)
	}
	return string(data)
}

func TestErrorResponse(t *testing.T) {
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
	})
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
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`)
	})
	defer done()

	out, err := client.Complete(context.Background(), Request{Model: "m", Prompt: "hi"})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if out.Text != "ok" || calls != 2 {
		t.Errorf("Complete() = %q after %d calls, want %q after 2", out.Text, calls, "ok")
	}
}

// Every provider is reached at the same path — Anthropic included, via its
// OpenAI-compatible endpoint. Only the base URL differs.
func TestEndpointIsTheSameEverywhere(t *testing.T) {
	for _, baseURL := range []string{
		"https://api.anthropic.com",
		"https://api.openai.com",
		"http://127.0.0.1:11434",
	} {
		want := baseURL + "/v1/chat/completions"
		if got := Endpoint(&pslrc.Model{BaseURL: baseURL}); got != want {
			t.Errorf("Endpoint(%q) = %q, want %q", baseURL, got, want)
		}
	}
}
