package compiler

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"psl/internal/llm"
	"psl/internal/psllog"
	"psl/internal/pslrc"
)

// searchingClient stands in for a model that answers by searching once and then
// writing. It runs the searcher itself, the way the real client does, so the
// compiler is tested on what it hands over and what it gets back.
type searchingClient struct {
	query string
	reply string
	got   llm.Request
}

func (c *searchingClient) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	c.got = req
	out := &llm.Response{Text: c.reply, StopReason: "stop"}
	if req.Search == nil {
		return out, nil
	}
	result, err := req.Search.Search(ctx, c.query)
	if err != nil {
		out.Searches = []llm.SearchResult{{Query: c.query, Error: err.Error()}}
		return out, nil
	}
	out.Searches = []llm.SearchResult{*result}
	return out, nil
}

type fakeSearcher struct {
	answer  string
	sources []llm.Source
	asked   []string
	model   string
}

func (s *fakeSearcher) Search(_ context.Context, query string) (*llm.SearchResult, error) {
	s.asked = append(s.asked, query)
	return &llm.SearchResult{Query: query, Answer: s.answer, Sources: s.sources}, nil
}

func searchConfig(t *testing.T, extra string) *pslrc.Config {
	t.Helper()
	cfg, err := pslrc.Parse(`default_model=gpt-5.6-luna

[gpt-5.6-luna]
base_url=https://api.openai.com
api_key=sk-o
`+extra, ".pslrc")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// A section with web_search on gets the tool, and the slot's system prompt says
// so — a tool the model is not told about is one it will not think to use.
func TestCompileOffersSearchWhenConfigured(t *testing.T) {
	t.Setenv(pslrc.EnvOpenAIKey, "")
	path := writeSource(t, "package main\n\nconst GoVersion = :: the current stable Go release, as a quoted string ::\n")
	client := &searchingClient{query: "latest stable Go release", reply: `"1.26.5"`}
	searcher := &fakeSearcher{answer: "Go 1.26.5", sources: []llm.Source{{Title: "All releases", URL: "https://go.dev/dl/"}}}

	result, err := Compile(context.Background(), Options{
		Path:        path,
		Config:      searchConfig(t, "web_search=on\n"),
		NewClient:   func(*pslrc.Model) llm.Client { return client },
		NewSearcher: func(m *pslrc.Model) llm.Searcher { searcher.model = m.Name; return searcher },
	})
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	if client.got.Search == nil {
		t.Fatal("the request carried no searcher, want the tool offered")
	}
	if !strings.Contains(client.got.System, llm.SearchToolName) {
		t.Errorf("system prompt does not mention %s:\n%s", llm.SearchToolName, client.got.System)
	}
	// web_search=on names no model, so it is the default search model that runs.
	if searcher.model != pslrc.DefaultSearchModel {
		t.Errorf("searched with %q, want %q", searcher.model, pslrc.DefaultSearchModel)
	}
	if len(result.Searches) != 1 || result.Searches[0].Answer != "Go 1.26.5" {
		t.Errorf("Searches = %+v, want the search the model made", result.Searches)
	}
	if got := read(t, path); !strings.Contains(got, `const GoVersion = "1.26.5"`) {
		t.Errorf("file =\n%s\nwant the searched value written into it", got)
	}
}

// Search off is the default, and a compiler that quietly went to the web would
// be spending money the author did not ask it to.
func TestCompileDoesNotSearchByDefault(t *testing.T) {
	path := writeSource(t, "package main\n\nfunc f() {\n\t:: return ::\n}\n")
	client := &searchingClient{reply: "return"}

	result, err := Compile(context.Background(), Options{
		Path:      path,
		Config:    searchConfig(t, ""),
		NewClient: func(*pslrc.Model) llm.Client { return client },
		NewSearcher: func(*pslrc.Model) llm.Searcher {
			t.Fatal("a searcher was built for a section that left web_search off")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	if client.got.Search != nil {
		t.Error("the request carried a searcher, want none")
	}
	if strings.Contains(client.got.System, llm.SearchToolName) {
		t.Error("the system prompt offered a tool that was not there")
	}
	if len(result.Searches) != 0 {
		t.Errorf("Searches = %+v, want none", result.Searches)
	}
}

// A search endpoint that cannot be resolved is worth failing on before the slot
// is sent, not after it has been paid for and the tool call has nowhere to go.
func TestCompileFailsBeforeSpendingOnAnUnresolvableSearch(t *testing.T) {
	path := writeSource(t, "package main\n\nfunc f() {\n\t:: return ::\n}\n")
	source := read(t, path)
	client := &searchingClient{reply: "return"}

	_, err := Compile(context.Background(), Options{
		Path:      path,
		Config:    searchConfig(t, "web_search=nowhere\n"),
		NewClient: func(*pslrc.Model) llm.Client { return client },
	})
	if err == nil || !strings.Contains(err.Error(), "web_search") {
		t.Fatalf("Compile() error = %v, want the unresolvable search reported", err)
	}
	if client.got.Model != "" {
		t.Error("the model was called anyway, want the run stopped first")
	}
	if read(t, path) != source {
		t.Error("the file was rewritten by a run that failed")
	}
}

// The log is where an author checks what a generated line rested on, so a
// searched slot records the queries, the answers and the pages.
func TestSearchesAreLogged(t *testing.T) {
	t.Setenv(pslrc.EnvOpenAIKey, "")
	home := t.TempDir()
	logger, err := psllog.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	path := writeSource(t, "package main\n\nconst V = :: the current Go release ::\n")
	client := &searchingClient{query: "latest stable Go release", reply: `"1.26.5"`}
	searcher := &fakeSearcher{answer: "Go 1.26.5", sources: []llm.Source{{Title: "All releases", URL: "https://go.dev/dl/"}}}

	if _, err := Compile(context.Background(), Options{
		Path:        path,
		Config:      searchConfig(t, "web_search=on\n"),
		Log:         logger,
		NewClient:   func(*pslrc.Model) llm.Client { return client },
		NewSearcher: func(*pslrc.Model) llm.Searcher { return searcher },
	}); err != nil {
		t.Fatalf("Compile() error: %v", err)
	}

	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatal(err)
	}
	var entry psllog.Entry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &entry); err != nil {
		t.Fatalf("decode log line: %v", err)
	}
	if len(entry.Searches) != 1 {
		t.Fatalf("logged %d searches, want 1: %s", len(entry.Searches), data)
	}
	search := entry.Searches[0]
	if search.Query != "latest stable Go release" || search.Answer != "Go 1.26.5" {
		t.Errorf("logged search = %+v, want the query and what came back", search)
	}
	if len(search.Sources) != 1 || search.Sources[0] != "https://go.dev/dl/" {
		t.Errorf("logged sources = %v, want the page the answer rested on", search.Sources)
	}
	// Which model did the searching is part of explaining the line later.
	if entry.Model.WebSearch != pslrc.DefaultSearchModel {
		t.Errorf("logged web_search = %q, want %q", entry.Model.WebSearch, pslrc.DefaultSearchModel)
	}
}
