// Package llm talks to the chat API that resolves a PSL slot.
//
// There is one wire protocol: OpenAI's chat completions. Every provider psl
// supports speaks it — Anthropic through its OpenAI-compatible endpoint — so a
// model differs from another only by base URL, key, and id.
//
// Web search holds to the same rule. It is offered as an ordinary function tool
// and answered here, in [Searcher], rather than through a provider's own hosted
// search: those disagree on a spelling and on an API, and chat completions
// takes none of them. Function calling is what every endpoint has in common, so
// search costs the package a tool loop and no second protocol.
package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"psl/internal/pslrc"
)

// DefaultMaxTokens bounds a single slot's generated output.
const DefaultMaxTokens = 8192

// Timeout is the per-request deadline.
const Timeout = 5 * time.Minute

// chatPath is the endpoint on every base URL that psl posts to.
const chatPath = "/v1/chat/completions"

// Image is visual context handed to the slot being resolved.
type Image struct {
	MediaType string // e.g. "image/png"
	Base64    string // raw base64, no data: prefix
}

// Request is one completion.
type Request struct {
	Model     string
	System    string
	Prompt    string
	Image     *Image
	MaxTokens int
	// Params are the model's `params=` from .pslrc, merged into the body
	// beside the fields psl builds. See pslrc.Model.Params.
	Params map[string]any
	// Search, when set, offers the model a web_search tool and answers the
	// calls it makes with it. Nil leaves the request without tools, which is
	// every request from a section that did not set `web_search=`.
	Search Searcher
}

// Usage is what the model reported spending on a request.
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// add accumulates another round's usage. Resolving one slot can take several
// requests when the model searches, and what the slot cost is all of them.
func (u *Usage) add(other Usage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.TotalTokens += other.TotalTokens
}

// Response is a completed request.
type Response struct {
	Text       string
	StopReason string
	Usage      Usage
	// Sources are the pages the answer cited, for endpoints that report them.
	// A search model fills this in; an ordinary completion leaves it empty.
	Sources []Source
	// Searches are the web_search calls made while this request was answered,
	// in the order they were asked. Empty unless Search was set.
	Searches []SearchResult
}

// Client is a chat endpoint.
type Client interface {
	Complete(ctx context.Context, req Request) (*Response, error)
}

// Endpoint reports the URL a model's requests are sent to.
func Endpoint(m *pslrc.Model) string {
	return m.BaseURL + chatPath
}

// Body renders the JSON an endpoint receives for req. Credentials are not in
// it: those travel in headers.
//
// This is what the log records, so an attached image is reduced to a note of
// its size; megabytes of base64 on one line would make the log unreadable.
func Body(req Request) (json.RawMessage, error) {
	data, err := json.Marshal(openAIBody(elideImage(req)))
	if err != nil {
		return nil, fmt.Errorf("encode request body: %w", err)
	}
	return data, nil
}

// elideImage swaps an image's payload for a note of what it weighed, which is
// all a log can usefully keep of it.
func elideImage(req Request) Request {
	if req.Image == nil {
		return req
	}
	image := *req.Image
	image.Base64 = fmt.Sprintf("…%d bytes elided…", base64Size(image.Base64))
	req.Image = &image
	return req
}

// base64Size is the number of bytes a base64 payload decodes to. DecodedLen
// alone only bounds it, since it cannot see the padding.
func base64Size(payload string) int {
	return base64.StdEncoding.DecodedLen(len(payload)) - strings.Count(payload, "=")
}

// New builds the client for a configured model.
func New(m *pslrc.Model) Client {
	return &openAIClient{&base{baseURL: m.BaseURL, apiKey: m.APIKey, http: &http.Client{Timeout: Timeout}}}
}

type base struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// post sends body to path and decodes the JSON response into out. Transient
// failures (429 and 5xx) are retried a few times with a fixed backoff.
func (b *base) post(ctx context.Context, path string, header http.Header, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	url := b.baseURL + path

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req.Header = header.Clone()
		req.Header.Set("Content-Type", "application/json")

		resp, err := b.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("call %s: %w", url, err)
			continue
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read response from %s: %w", url, readErr)
			continue
		}
		if resp.StatusCode/100 != 2 {
			err := fmt.Errorf("%s returned %s: %s", url, resp.Status, snippet(data))
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode/100 == 5 {
				lastErr = err
				continue
			}
			return &statusError{code: resp.StatusCode, err: err}
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response from %s: %w", url, err)
		}
		return nil
	}
	return lastErr
}

// statusError is an endpoint refusing a request, as against a network that
// could not carry one. Only a refusal says something about what was sent, and
// so only a refusal is worth explaining in psl's own terms.
type statusError struct {
	code int
	err  error
}

func (e *statusError) Error() string { return e.err.Error() }
func (e *statusError) Unwrap() error { return e.err }

// refused reports whether err is an endpoint rejecting the request itself.
func refused(err error) bool {
	var status *statusError
	return errors.As(err, &status) && status.code/100 == 4
}

func snippet(data []byte) string {
	s := strings.TrimSpace(string(data))
	if len(s) > 500 {
		s = s[:500] + "…"
	}
	if s == "" {
		s = "(empty body)"
	}
	return s
}

func maxTokens(req Request) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	return DefaultMaxTokens
}
