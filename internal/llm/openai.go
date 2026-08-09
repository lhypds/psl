package llm

import (
	"context"
	"fmt"
	"net/http"
)

type openAIClient struct{ *base }

type openAIRequest struct {
	Model               string          `json:"model"`
	MaxCompletionTokens int             `json:"max_completion_tokens"`
	Messages            []openAIMessage `json:"messages"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string, or []openAIPart when an image is attached
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
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content string `json:"content"`
		} `json:"message"`
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

// openAIBody is the request the chat completions API receives for req.
func openAIBody(req Request) openAIRequest {
	return openAIRequest{
		Model:               req.Model,
		MaxCompletionTokens: maxTokens(req),
		Messages:            openAIMessages(req),
	}
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

func (c *openAIClient) Complete(ctx context.Context, req Request) (*Response, error) {
	body := openAIBody(req)

	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.apiKey)

	var resp openAIResponse
	if err := c.post(ctx, chatPath, header, body, &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("model %s: %s", req.Model, resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("model %s returned no choices", req.Model)
	}
	choice := resp.Choices[0]
	if choice.Message.Content == "" {
		return nil, fmt.Errorf("model %s returned empty content (finish_reason %q)", req.Model, choice.FinishReason)
	}
	if choice.FinishReason == "length" {
		return nil, fmt.Errorf("model %s hit the token limit (%d) before finishing; raise max_tokens in .pslrc",
			req.Model, maxTokens(req))
	}
	return &Response{
		Text:       choice.Message.Content,
		StopReason: choice.FinishReason,
		Usage: Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
	}, nil
}
