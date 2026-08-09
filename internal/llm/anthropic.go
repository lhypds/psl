package llm

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// anthropicVersion is the API version header required by the Messages API.
const anthropicVersion = "2023-06-01"

type anthropicClient struct{ *base }

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type   string           `json:"type"`
	Text   string           `json:"text,omitempty"`
	Source *anthropicSource `json:"source,omitempty"`
}

type anthropicSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
}

func (c *anthropicClient) Complete(ctx context.Context, req Request) (string, error) {
	content := make([]anthropicContent, 0, 2)
	if req.Image != nil {
		content = append(content, anthropicContent{
			Type: "image",
			Source: &anthropicSource{
				Type:      "base64",
				MediaType: req.Image.MediaType,
				Data:      req.Image.Base64,
			},
		})
	}
	content = append(content, anthropicContent{Type: "text", Text: req.Prompt})

	body := anthropicRequest{
		Model:     req.Model,
		MaxTokens: maxTokens(req),
		System:    req.System,
		Messages:  []anthropicMessage{{Role: "user", Content: content}},
	}

	header := http.Header{}
	header.Set("x-api-key", c.apiKey)
	header.Set("anthropic-version", anthropicVersion)

	var resp anthropicResponse
	if err := c.post(ctx, "/v1/messages", header, body, &resp); err != nil {
		return "", err
	}

	var text strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	if text.Len() == 0 {
		return "", fmt.Errorf("model %s returned no text (stop_reason %q)", req.Model, resp.StopReason)
	}
	if resp.StopReason == "max_tokens" {
		return "", fmt.Errorf("model %s hit max_tokens (%d) before finishing; raise max_tokens in .pslrc",
			req.Model, maxTokens(req))
	}
	return text.String(), nil
}
