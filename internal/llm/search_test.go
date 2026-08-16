package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"psl/internal/pslrc"
)

// stubSearcher answers every query from a script, and remembers what it was
// asked so a test can check the model's query reached it intact.
type stubSearcher struct {
	answer  string
	sources []Source
	err     error
	queries []string
}

func (s *stubSearcher) Search(_ context.Context, query string) (*SearchResult, error) {
	s.queries = append(s.queries, query)
	if s.err != nil {
		return nil, s.err
	}
	return &SearchResult{Query: query, Answer: s.answer, Sources: s.sources}, nil
}

// toolCallReply is a response asking for one web_search call.
func toolCallReply(id, query string) string {
	args, _ := json.Marshal(map[string]string{"query": query})
	call, _ := json.Marshal(map[string]any{
		"id": id, "type": "function",
		"function": map[string]string{"name": SearchToolName, "arguments": string(args)},
	})
	return fmt.Sprintf(`{"choices":[{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[%s]}}],
		"usage":{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110}}`, call)
}

// A section that left web_search off sends the request psl has always sent:
// offering a tool psl would have to answer is not something a compiler does by
// default.
func TestNoToolWithoutASearcher(t *testing.T) {
	var got map[string]any
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = decodeBody(t, r)
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`)
	})
	defer done()

	if _, err := client.Complete(context.Background(), Request{Model: "m", Prompt: "hi"}); err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if _, offered := got["tools"]; offered {
		t.Errorf("body carried tools with no searcher: %v", got["tools"])
	}
}

// The whole feature in one pass: the tool is offered, the model calls it, the
// query reaches the searcher, the answer comes back as a tool message, and the
// text the model then writes is what resolves the slot.
func TestSearchToolRoundTrip(t *testing.T) {
	search := &stubSearcher{
		answer:  "Go 1.26.5",
		sources: []Source{{Title: "All releases", URL: "https://go.dev/dl/"}},
	}
	var bodies []map[string]any
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body := decodeBody(t, r)
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			io.WriteString(w, toolCallReply("call_1", "latest stable Go release"))
			return
		}
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"const GoVersion = \"1.26.5\""}}],
			"usage":{"prompt_tokens":300,"completion_tokens":20,"total_tokens":320}}`)
	})
	defer done()

	out, err := client.Complete(context.Background(), Request{Model: "m", Prompt: "the file", Search: search})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if out.Text != `const GoVersion = "1.26.5"` {
		t.Errorf("Complete() = %q, want the text written after the search", out.Text)
	}
	if len(bodies) != 2 {
		t.Fatalf("made %d requests, want one to ask and one to answer", len(bodies))
	}

	// The tool is offered as an ordinary function, which is the one shape every
	// endpoint psl speaks to understands.
	tools, ok := bodies[0]["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want the one web_search tool", bodies[0]["tools"])
	}
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" {
		t.Errorf("tool type = %v, want function", tool["type"])
	}
	if name := tool["function"].(map[string]any)["name"]; name != SearchToolName {
		t.Errorf("tool name = %v, want %s", name, SearchToolName)
	}

	if want := []string{"latest stable Go release"}; len(search.queries) != 1 || search.queries[0] != want[0] {
		t.Errorf("searcher was asked %q, want %q", search.queries, want)
	}

	// The second request replays the assistant's call and answers it, or the
	// endpoint has no conversation to continue.
	messages := bodies[1]["messages"].([]any)
	last := messages[len(messages)-1].(map[string]any)
	if last["role"] != "tool" {
		t.Fatalf("last message = %v, want the tool's answer", last)
	}
	if last["tool_call_id"] != "call_1" {
		t.Errorf("tool_call_id = %v, want the id of the call it answers", last["tool_call_id"])
	}
	content := last["content"].(string)
	if !strings.Contains(content, "Go 1.26.5") || !strings.Contains(content, "https://go.dev/dl/") {
		t.Errorf("tool message = %q, want the answer and where it came from", content)
	}
	assistant := messages[len(messages)-2].(map[string]any)
	if assistant["role"] != "assistant" || assistant["tool_calls"] == nil {
		t.Errorf("second to last message = %v, want the assistant turn that asked", assistant)
	}
}

// A slot that took a search cost both requests, and `psl usage` is only honest
// if it is told so.
func TestUsageCoversEveryRound(t *testing.T) {
	calls := 0
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			io.WriteString(w, toolCallReply("call_1", "q"))
			return
		}
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}],
			"usage":{"prompt_tokens":300,"completion_tokens":20,"total_tokens":320}}`)
	})
	defer done()

	out, err := client.Complete(context.Background(), Request{Model: "m", Prompt: "p", Search: &stubSearcher{answer: "a"}})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if want := (Usage{InputTokens: 400, OutputTokens: 30, TotalTokens: 430}); out.Usage != want {
		t.Errorf("Usage = %+v, want both rounds added up (%+v)", out.Usage, want)
	}
	if len(out.Searches) != 1 || out.Searches[0].Answer != "a" {
		t.Errorf("Searches = %+v, want the one search recorded for the log", out.Searches)
	}
}

// A search that fails is not a compilation that fails: the model is told, and
// resolves the slot from what it already has.
func TestFailedSearchIsReportedToTheModel(t *testing.T) {
	var second map[string]any
	calls := 0
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			io.WriteString(w, toolCallReply("call_1", "q"))
			return
		}
		second = decodeBody(t, r)
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`)
	})
	defer done()

	out, err := client.Complete(context.Background(), Request{
		Model: "m", Prompt: "p",
		Search: &stubSearcher{err: fmt.Errorf("dial tcp: connection refused")},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v, want the slot resolved without the search", err)
	}
	if out.Text != "ok" {
		t.Errorf("Complete() = %q, want the model's own answer", out.Text)
	}
	messages := second["messages"].([]any)
	content := messages[len(messages)-1].(map[string]any)["content"].(string)
	if !strings.Contains(content, "connection refused") {
		t.Errorf("tool message = %q, want it to say why the search failed", content)
	}
	// The log has to show the search was tried and lost, not that none was made.
	if len(out.Searches) != 1 || out.Searches[0].Error == "" {
		t.Errorf("Searches = %+v, want the failure recorded", out.Searches)
	}
}

// A model that only ever searches would otherwise loop until the deadline, on
// the author's money.
func TestSearchRoundsAreBounded(t *testing.T) {
	calls := 0
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, toolCallReply(fmt.Sprintf("call_%d", calls), "q"))
	})
	defer done()

	_, err := client.Complete(context.Background(), Request{Model: "m", Prompt: "p", Search: &stubSearcher{answer: "a"}})
	if err == nil || !strings.Contains(err.Error(), "without resolving the slot") {
		t.Fatalf("Complete() error = %v, want it to stop searching eventually", err)
	}
	if want := MaxSearchRounds + 1; calls != want {
		t.Errorf("made %d requests, want %d — %d rounds of searching and the one that asked again",
			calls, want, MaxSearchRounds)
	}
}

// A tool call arriving on a request that offered no tool is the endpoint
// answering something psl did not ask, and there is nothing to run for it.
func TestToolCallWithoutASearcherIsAnError(t *testing.T) {
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, toolCallReply("call_1", "q"))
	})
	defer done()

	_, err := client.Complete(context.Background(), Request{Model: "m", Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "none was offered") {
		t.Fatalf("Complete() error = %v, want it to refuse a tool it never offered", err)
	}
}

// The searcher is a model like any other, reached the same way. What makes it a
// searcher is that its answers carry the pages they came from.
func TestSearcherReadsCitations(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = decodeBody(t, r)
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"Go 1.26.5",
			"annotations":[
				{"type":"url_citation","url_citation":{"title":"All releases","url":"https://go.dev/dl/"}},
				{"type":"url_citation","url_citation":{"title":"All releases","url":"https://go.dev/dl/"}}
			]}}]}`)
	}))
	defer srv.Close()

	searcher := NewSearcher(&pslrc.Model{Name: "search-model", BaseURL: srv.URL, APIKey: "sk-test"})
	result, err := searcher.Search(context.Background(), "latest Go release")
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if result.Answer != "Go 1.26.5" {
		t.Errorf("Answer = %q, want the search model's answer", result.Answer)
	}
	// One page cited twice is one source: what the answer rested on, not how
	// often the model pointed at it.
	if len(result.Sources) != 1 || result.Sources[0].URL != "https://go.dev/dl/" {
		t.Errorf("Sources = %+v, want the one page it came from", result.Sources)
	}
	// The searcher must never be handed the tool it exists to answer, or a
	// search would be able to start another one.
	if _, offered := got["tools"]; offered {
		t.Errorf("the search request offered tools: %v", got["tools"])
	}
	if got["model"] != "search-model" {
		t.Errorf("model = %v, want the section configured to search", got["model"])
	}
}

// Reasoning and tool calling are not always both on offer — OpenAI's gpt-5.6
// family refuses function tools on this endpoint unless reasoning is off — so
// psl turns it off wherever it puts a tool in the request. One switch in .pslrc
// must not produce a request the endpoint will not take.
func TestSearchTurnsReasoningOff(t *testing.T) {
	var got map[string]any
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = decodeBody(t, r)
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`)
	})
	defer done()

	if _, err := client.Complete(context.Background(), Request{
		Model: "m", Prompt: "p", Search: &stubSearcher{answer: "a"},
	}); err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if got["reasoning_effort"] != "none" {
		t.Errorf("reasoning_effort = %v, want none alongside the tool", got["reasoning_effort"])
	}
}

// A section that never asked for search is untouched by any of it: the field is
// not psl's to set on a request it did not put a tool in.
func TestNoReasoningFieldWithoutSearch(t *testing.T) {
	var got map[string]any
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = decodeBody(t, r)
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`)
	})
	defer done()

	if _, err := client.Complete(context.Background(), Request{Model: "m", Prompt: "p"}); err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if _, set := got["reasoning_effort"]; set {
		t.Errorf("reasoning_effort = %v on a request with no tool, want the field absent", got["reasoning_effort"])
	}
}

// It is a default, not a decision. An endpoint that takes tools and reasoning
// together gets its reasoning back from params=, which wins as it always does.
func TestParamsOverrideTheReasoningDefault(t *testing.T) {
	var got map[string]any
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = decodeBody(t, r)
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`)
	})
	defer done()

	if _, err := client.Complete(context.Background(), Request{
		Model: "m", Prompt: "p",
		Search: &stubSearcher{answer: "a"},
		Params: map[string]any{"reasoning_effort": "high"},
	}); err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if got["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %v, want the section's own value", got["reasoning_effort"])
	}
}

// An endpoint that has never heard of the field needs it gone, not nulled, and
// a null in params is the way to take back anything psl defaulted.
func TestParamsNullRemovesAField(t *testing.T) {
	var got map[string]any
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = decodeBody(t, r)
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`)
	})
	defer done()

	if _, err := client.Complete(context.Background(), Request{
		Model: "m", Prompt: "p",
		Search: &stubSearcher{answer: "a"},
		Params: map[string]any{"reasoning_effort": nil},
	}); err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if _, set := got["reasoning_effort"]; set {
		t.Errorf("reasoning_effort = %v, want the field dropped entirely", got["reasoning_effort"])
	}
	// Dropping one field must not disturb the request around it.
	if got["model"] != "m" || got["tools"] == nil {
		t.Errorf("body = %#v, want the rest of the request intact", got)
	}
}

// Function calling is what psl relies on to search anywhere, so it has to
// survive the ways an OpenAI-compatible server differs from OpenAI: arguments
// sent as the object itself rather than as a JSON string, and a tool call with
// no type on it.
func TestToolCallFromALooseEndpoint(t *testing.T) {
	search := &stubSearcher{answer: "Go 1.26.5"}
	calls := 0
	var second map[string]any
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"content":"",
				"tool_calls":[{"id":"c1","function":{"name":"web_search",
				"arguments":{"query":"latest Go release"}}}]}}]}`)
			return
		}
		second = decodeBody(t, r)
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`)
	})
	defer done()

	out, err := client.Complete(context.Background(), Request{Model: "m", Prompt: "p", Search: search})
	if err != nil {
		t.Fatalf("Complete() error = %v, want the object arguments read anyway", err)
	}
	if out.Text != "ok" {
		t.Errorf("Complete() = %q, want the slot resolved", out.Text)
	}
	if len(search.queries) != 1 || search.queries[0] != "latest Go release" {
		t.Errorf("searcher was asked %q, want the query out of the object", search.queries)
	}
	// What goes back is the protocol's own shape, whatever shape arrived.
	messages := second["messages"].([]any)
	assistant := messages[len(messages)-2].(map[string]any)
	call := assistant["tool_calls"].([]any)[0].(map[string]any)
	if call["type"] != "function" {
		t.Errorf("echoed type = %v, want function filled in", call["type"])
	}
	args, ok := call["function"].(map[string]any)["arguments"].(string)
	if !ok {
		t.Fatalf("echoed arguments = %#v, want the JSON string the protocol asks for", call["function"])
	}
	if !strings.Contains(args, "latest Go release") {
		t.Errorf("echoed arguments = %q, want the query preserved", args)
	}
}

// A model asking for a tool psl never offered gets told so, in the reply, and
// carries on — the alternative is failing a slot over a name.
func TestUnknownToolIsAnsweredNotFatal(t *testing.T) {
	calls := 0
	var second map[string]any
	client, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"content":"",
				"tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{}"}}]}}]}`)
			return
		}
		second = decodeBody(t, r)
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`)
	})
	defer done()

	if _, err := client.Complete(context.Background(), Request{
		Model: "m", Prompt: "p", Search: &stubSearcher{answer: "a"},
	}); err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	messages := second["messages"].([]any)
	content := messages[len(messages)-1].(map[string]any)["content"].(string)
	if !strings.Contains(content, "read_file") || !strings.Contains(content, SearchToolName) {
		t.Errorf("tool message = %q, want it to name the tool that does exist", content)
	}
}
