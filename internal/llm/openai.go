package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type openAIClient struct{ *base }

type openAIRequest struct {
	Model               string          `json:"model"`
	MaxCompletionTokens int             `json:"max_completion_tokens"`
	Messages            []openAIMessage `json:"messages"`
	// Tools is the web_search tool, present only while a section has search
	// turned on. Left out entirely otherwise, so a request from a section
	// without it is byte for byte the request psl always sent.
	Tools []openAITool `json:"tools,omitempty"`
	// ReasoningEffort is "none" wherever the search tool is offered.
	//
	// Reasoning and tool calling are not always both available at once: OpenAI's
	// gpt-5.6 family refuses function tools on this endpoint outright unless
	// reasoning is off. Since psl is what put the tool in the request, psl is
	// what turns reasoning off for it, rather than letting one switch in .pslrc
	// produce a request the endpoint will not take.
	//
	// It is a default and not a decision: `params=` sets it back for an endpoint
	// that has no such trouble, and a null there drops the field entirely for
	// one that has never heard of it. Absent when no tool is offered, so a
	// section without search is untouched by any of this.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// Params are `params=` from .pslrc, written into the body beside the fields
	// above rather than under a key of their own: they are the request's
	// own fields, and an endpoint reading temperature does not read it out of a
	// nested object. Merged by MarshalJSON, which is why they carry no tag.
	Params map[string]any `json:"-"`
}

type openAITool struct {
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// openAIToolCall is one call the model made.
type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string        `json:"name"`
		Arguments toolArguments `json:"arguments"`
	} `json:"function"`
}

// toolArguments is a call's arguments. The protocol carries them as a JSON
// string rather than an object, and most endpoints send one — but psl is
// pointed at whatever speaks this protocol, and some OpenAI-compatible servers
// send the object itself. Reading both costs a method; reading only the string
// would fail the whole response over the shape of one field.
type toolArguments string

func (a *toolArguments) UnmarshalJSON(data []byte) error {
	var encoded string
	if err := json.Unmarshal(data, &encoded); err == nil {
		*a = toolArguments(encoded)
		return nil
	}
	*a = toolArguments(data)
	return nil
}

// MarshalJSON writes the arguments back as the string the protocol asks for,
// which is the form every endpoint accepts whatever it sends itself.
func (a toolArguments) MarshalJSON() ([]byte, error) { return json.Marshal(string(a)) }

// echo prepares a call to be sent back in the assistant turn that made it. The
// type is the only field psl fills in: it is "function" for every tool psl
// offers, and an endpoint that left it out is still owed a valid message.
func (c openAIToolCall) echo() openAIToolCall {
	if c.Type == "" {
		c.Type = "function"
	}
	return c
}

// MarshalJSON writes the request with its configured params merged in.
//
// The fields psl builds cannot be overwritten here — pslrc refuses a params
// that names one — so a merge only ever adds, and what a section asked for
// arrives spelled exactly as it was written.
func (r openAIRequest) MarshalJSON() ([]byte, error) {
	// A defined type off openAIRequest carries the field tags and none of the
	// methods, so marshalling that is the plain encoding rather than this
	// method calling itself.
	type fields openAIRequest
	data, err := json.Marshal(fields(r))
	if err != nil {
		return nil, err
	}
	if len(r.Params) == 0 {
		return data, nil
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}
	for name, value := range r.Params {
		// A null removes the field rather than sending one. It is the only way
		// to take back something psl defaulted — reasoning_effort, for a model
		// that has never heard of it — and sending a literal null instead would
		// be the same request the endpoint already refused.
		if value == nil {
			delete(body, name)
			continue
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("params %s: %w", name, err)
		}
		body[name] = raw
	}
	return json.Marshal(body)
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string, or []openAIPart when an image is attached
	// ToolCalls carries an assistant turn's calls back to the endpoint, which
	// only continues a conversation it can see itself having asked for.
	ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
	// ToolCallID names the call a tool message answers.
	ToolCallID string `json:"tool_call_id,omitempty"`
}

type openAIPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *openAIImageURL `json:"image_url,omitempty"`
}

type openAIImageURL struct {
	URL string `json:"url"`
}

type openAIResponse struct {
	Choices []struct {
		FinishReason string        `json:"finish_reason"`
		Message      openAIReplied `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// openAIReplied is the assistant message a choice carries.
type openAIReplied struct {
	Content   string           `json:"content"`
	ToolCalls []openAIToolCall `json:"tool_calls"`
	// Annotations are the pages a search model drew on, which it reports
	// alongside the answer rather than inside it.
	Annotations []openAIAnnotation `json:"annotations"`
}

type openAIAnnotation struct {
	Type        string `json:"type"`
	URLCitation struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	} `json:"url_citation"`
}

// sources are the cited pages, deduplicated: a model that cites one page twice
// in an answer reports it twice here.
func (m openAIReplied) sources() []Source {
	var out []Source
	seen := map[string]bool{}
	for _, a := range m.Annotations {
		url := a.URLCitation.URL
		if a.Type != "url_citation" || url == "" || seen[url] {
			continue
		}
		seen[url] = true
		out = append(out, Source{Title: a.URLCitation.Title, URL: url})
	}
	return out
}

// openAIBody is the request the chat completions API receives for req.
func openAIBody(req Request) openAIRequest {
	return openAIBodyWith(req, openAIMessages(req))
}

// openAIBodyWith is openAIBody over a conversation already in progress, which
// is what a second round after a search is.
func openAIBodyWith(req Request, messages []openAIMessage) openAIRequest {
	body := openAIRequest{
		Model:               req.Model,
		MaxCompletionTokens: maxTokens(req),
		Messages:            messages,
		Tools:               searchTools(req),
		Params:              req.Params,
	}
	if req.Search != nil {
		body.ReasoningEffort = noReasoning
	}
	return body
}

// openAIMessages is the message list for req. The system prompt is a message
// of its own, and an image rides along as a data: URL.
func openAIMessages(req Request) []openAIMessage {
	messages := make([]openAIMessage, 0, 2)
	if req.System != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: req.System})
	}
	if req.Image == nil {
		messages = append(messages, openAIMessage{Role: "user", Content: req.Prompt})
	} else {
		messages = append(messages, openAIMessage{Role: "user", Content: []openAIPart{
			{Type: "image_url", ImageURL: &openAIImageURL{
				URL: fmt.Sprintf("data:%s;base64,%s", req.Image.MediaType, req.Image.Base64),
			}},
			{Type: "text", Text: req.Prompt},
		}})
	}
	return messages
}

// Complete resolves req, going round again for as long as the model answers
// with searches rather than with text. Without a Searcher there is only ever
// one round, which is the request psl has always made.
func (c *openAIClient) Complete(ctx context.Context, req Request) (*Response, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.apiKey)

	messages := openAIMessages(req)
	// A slot's cost is every round it took, so usage accumulates across them
	// rather than being read off the last one.
	var usage Usage
	var searches []SearchResult
	searchRounds := 0

	for {
		var resp openAIResponse
		if err := c.post(ctx, chatPath, header, openAIBodyWith(req, messages), &resp); err != nil {
			return nil, explainRefusal(req, err)
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("model %s: %s", req.Model, resp.Error.Message)
		}
		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("model %s returned no choices", req.Model)
		}
		usage.add(Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		})
		choice := resp.Choices[0]

		if len(choice.Message.ToolCalls) > 0 {
			// A model cannot call a tool that was not offered, so this is an
			// endpoint inventing one rather than something to answer.
			if req.Search == nil {
				return nil, fmt.Errorf("model %s asked to call a tool, but none was offered", req.Model)
			}
			if searchRounds >= MaxSearchRounds {
				return nil, fmt.Errorf("model %s searched %d times without resolving the slot; "+
					"narrow the instruction, or turn web_search off for it in .pslrc", req.Model, MaxSearchRounds)
			}
			searchRounds++
			echoed := make([]openAIToolCall, len(choice.Message.ToolCalls))
			for i, call := range choice.Message.ToolCalls {
				echoed[i] = call.echo()
			}
			messages = append(messages, openAIMessage{
				Role:      "assistant",
				Content:   choice.Message.Content,
				ToolCalls: echoed,
			})
			answers, results := runSearches(ctx, req.Search, choice.Message.ToolCalls)
			messages = append(messages, answers...)
			searches = append(searches, results...)
			continue
		}

		// The token limit is reported before the empty content it causes, since
		// a truncated answer explains itself and an empty one does not.
		if choice.FinishReason == "length" {
			return nil, fmt.Errorf("model %s hit the token limit (%d) before finishing; raise max_tokens in .pslrc",
				req.Model, maxTokens(req))
		}
		if choice.Message.Content == "" {
			return nil, fmt.Errorf("model %s returned empty content (finish_reason %q)", req.Model, choice.FinishReason)
		}
		return &Response{
			Text:       choice.Message.Content,
			StopReason: choice.FinishReason,
			Usage:      usage,
			Sources:    choice.Message.sources(),
			Searches:   searches,
		}, nil
	}
}
