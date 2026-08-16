package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"psl/internal/pslrc"
)

// SearchToolName is the function a model calls to reach the web. It is offered
// as an ordinary function tool rather than a provider's own hosted one: every
// endpoint psl speaks to understands function calling, and the hosted web
// search tools do not agree on a spelling — or on an API. Chat completions
// rejects `{"type": "web_search"}` outright.
const SearchToolName = "web_search"

// MaxSearchRounds bounds how many times a slot may go back to the model with
// search results. A round can hold several queries, so this is a stop for a
// model that keeps searching rather than a budget the author is meant to spend.
const MaxSearchRounds = 4

// noReasoning is what a request offering the search tool asks for, since a
// model that reasons is not always one that will take a tool. See
// openAIRequest.ReasoningEffort.
const noReasoning = "none"

// searchDescription tells the model when reaching for the web is the right
// move. A slot is resolved once, at compile time, and its output is frozen into
// the file — so what matters is the fact being current now, not the model
// having a general licence to browse.
const searchDescription = `Search the web and get back an answer with the pages it came from.

Use it whenever resolving the slot depends on something the file cannot tell you and your own knowledge may be stale: the current release of a language or library, an API's present signature, a package's latest version, a live fact about the world. Ask one focused question per call, and call it more than once rather than folding several questions into one query.

Write the query the way someone would type it into a search engine, and leave words like "today", "latest" and "current" standing as they are. Do not work out a date and put it in the query: you do not reliably know today's date, and a query anchored to one only matches pages that happen to carry it — "today's news" finds the news, "news of 14 March 2026" finds nothing. The search knows when it is being run.`

// searchSystem is the system prompt of the search model itself. It answers a
// question for a compiler, not for a reader, so what is wanted is the fact and
// where it came from.
const searchSystem = `Answer the question from the web as briefly as it can be answered correctly.

State the fact itself — the version string, the signature, the value — rather than describing where it may be found. If the web does not settle the question, say so plainly instead of guessing. No preamble, no restatement of the question.`

// Source is one page an answer was drawn from.
type Source struct {
	Title string `json:"title,omitempty"`
	URL   string `json:"url"`
}

// SearchResult is one query the model asked and what came back. A failed search
// is a result too: the model is told the search failed and goes on without it,
// since a slot that can still be resolved from the file should not be lost to a
// search that timed out.
type SearchResult struct {
	Query   string   `json:"query"`
	Answer  string   `json:"answer,omitempty"`
	Sources []Source `json:"sources,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// Searcher answers the web_search calls a model makes while a slot is resolved.
type Searcher interface {
	Search(ctx context.Context, query string) (*SearchResult, error)
}

// NewSearcher builds the searcher backed by a configured model. The model is
// reached over the same protocol as any other — a search model differs from a
// chat model only in going to the web before it answers.
func NewSearcher(m *pslrc.Model) Searcher {
	return &modelSearcher{model: m, client: New(m)}
}

type modelSearcher struct {
	model  *pslrc.Model
	client Client
}

// Search asks the search model one query. Search is left nil on the request it
// builds, so the searcher can never be handed a tool that calls itself.
func (s *modelSearcher) Search(ctx context.Context, query string) (*SearchResult, error) {
	out, err := s.client.Complete(ctx, Request{
		Model:     s.model.Name,
		System:    searchSystem,
		Prompt:    query,
		MaxTokens: s.model.MaxTokens,
		Params:    s.model.Params,
	})
	if err != nil {
		return nil, err
	}
	return &SearchResult{Query: query, Answer: out.Text, Sources: out.Sources}, nil
}

// message renders a result as the tool message the model reads. Sources are
// listed under the answer so the model can cite one in a comment when the file
// calls for it, and so the log shows what the answer rested on.
func (r SearchResult) message() string {
	if r.Error != "" {
		return fmt.Sprintf("The web search failed: %s\nResolve the slot from what the file and your own knowledge give you, and do not present a guess as something you looked up.", r.Error)
	}
	var b strings.Builder
	b.WriteString(r.Answer)
	if len(r.Sources) > 0 {
		b.WriteString("\n\nSources:")
		for _, s := range r.Sources {
			if s.Title != "" {
				fmt.Fprintf(&b, "\n- %s — %s", s.Title, s.URL)
				continue
			}
			fmt.Fprintf(&b, "\n- %s", s.URL)
		}
	}
	return b.String()
}

// explainRefusal adds what psl knows to an endpoint's rejection of a request
// carrying the search tool.
//
// The endpoint's own message is kept — it is the one that knows what it
// objected to — and what follows is the part only psl can say: that a setting
// in .pslrc is what put a tool in the request at all. Not every model takes
// function tools on this endpoint, and some take them only with their reasoning
// turned down; a slot that suddenly fails after web_search was switched on
// should not read as the model refusing the file.
func explainRefusal(req Request, err error) error {
	if req.Search == nil || !refused(err) {
		return err
	}
	return fmt.Errorf("%w\n\tpsl offered this model a %s tool because its section in .pslrc sets web_search=. "+
		"A model that will not take function tools here cannot search: turn web_search off for it, move the slot "+
		"to a model that will, or — if the endpoint asked for a particular setting above — add it with params=",
		err, SearchToolName)
}

// searchTools is the tool list a request carries, which is the one tool psl
// offers and only when the model's section turned it on.
func searchTools(req Request) []openAITool {
	if req.Search == nil {
		return nil
	}
	return []openAITool{{
		Type: "function",
		Function: openAIToolFunction{
			Name:        SearchToolName,
			Description: searchDescription,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "What to look up, written as a question or a search phrase.",
					},
				},
				"required":             []string{"query"},
				"additionalProperties": false,
			},
		},
	}}
}

// runSearches answers one round of tool calls. Each call becomes a tool message
// replying to it: a model that asked is owed an answer for every call it made,
// or the next request is malformed.
func runSearches(ctx context.Context, search Searcher, calls []openAIToolCall) ([]openAIMessage, []SearchResult) {
	messages := make([]openAIMessage, 0, len(calls))
	results := make([]SearchResult, 0, len(calls))
	for _, call := range calls {
		result := answerCall(ctx, search, call)
		results = append(results, result)
		messages = append(messages, openAIMessage{
			Role:       "tool",
			ToolCallID: call.ID,
			Content:    result.message(),
		})
	}
	return messages, results
}

// answerCall runs a single tool call. Everything that can go wrong with it —
// the wrong tool, unreadable arguments, a search that failed — comes back as a
// result carrying the reason, because the model is the one that has to act on
// it and it is holding the only copy of what it meant to ask.
func answerCall(ctx context.Context, search Searcher, call openAIToolCall) SearchResult {
	if call.Function.Name != SearchToolName {
		return SearchResult{
			Error: fmt.Sprintf("there is no tool called %q; the only tool is %s", call.Function.Name, SearchToolName),
		}
	}
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return SearchResult{Error: fmt.Sprintf("could not read the arguments %s: %v", call.Function.Arguments, err)}
	}
	if strings.TrimSpace(args.Query) == "" {
		return SearchResult{Error: "the query was empty"}
	}
	result, err := search.Search(ctx, args.Query)
	if err != nil {
		return SearchResult{Query: args.Query, Error: err.Error()}
	}
	return *result
}
