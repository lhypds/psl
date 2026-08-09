// Package llm talks to the chat APIs that resolve a PSL slot.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
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
}

// Client is a chat endpoint.
type Client interface {
	Complete(ctx context.Context, req Request) (string, error)
}

// New builds the client for a configured model.
func New(m *pslrc.Model) (Client, error) {
	base := &base{baseURL: m.BaseURL, apiKey: m.APIKey, http: &http.Client{Timeout: Timeout}}
	switch m.Protocol() {
	case pslrc.APIAnthropic:
		return &anthropicClient{base}, nil
	case pslrc.APIOpenAI:
		return &openAIClient{base}, nil
	default:
		return nil, fmt.Errorf("unsupported api %q", m.Protocol())
	}
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
			return err
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response from %s: %w", url, err)
		}
		return nil
	}
	return lastErr
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
