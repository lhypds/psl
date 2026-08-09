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
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *openAIClient) Complete(ctx context.Context, req Request) (string, error) {
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

	body := openAIRequest{
		Model:               req.Model,
		MaxCompletionTokens: maxTokens(req),
		Messages:            messages,
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.apiKey)

	var resp openAIResponse
	if err := c.post(ctx, "/v1/chat/completions", header, body, &resp); err != nil {
		return "", err
	}
	if resp.Error != nil {
		return "", fmt.Errorf("model %s: %s", req.Model, resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("model %s returned no choices", req.Model)
	}
	choice := resp.Choices[0]
	if choice.Message.Content == "" {
		return "", fmt.Errorf("model %s returned empty content (finish_reason %q)", req.Model, choice.FinishReason)
	}
	if choice.FinishReason == "length" {
		return "", fmt.Errorf("model %s hit the token limit (%d) before finishing; raise max_tokens in .pslrc",
			req.Model, maxTokens(req))
	}
	return choice.Message.Content, nil
}
