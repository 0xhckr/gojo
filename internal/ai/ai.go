package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client calls the OpenRouter chat completions API.
type Client struct {
	apiKey string
	model  string
}

// NewClient creates a new OpenRouter client.
func NewClient(apiKey, model string) *Client {
	return &Client{apiKey: apiKey, model: model}
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

const commitPrompt = `You are a commit message generator. Given a diff, output ONLY the commit message text — no preamble, no markdown fencing, no explanation.

Rules:
- First line: short summary in imperative mood, ≤50 characters
- If the change needs context, add a blank line then a body (wrap at 72 chars)
- Be specific about what changed; avoid generic messages like "update files"
- If the diff is empty or trivial, output "chore: minor changes"`

// GenerateCommitMessage sends the diff to OpenRouter and returns a commit message.
func (c *Client) GenerateCommitMessage(ctx context.Context, diff string) (string, error) {
	// Truncate very large diffs to avoid token limits.
	if len(diff) > 10000 {
		diff = diff[:10000] + "\n... (truncated)"
	}

	reqBody := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "user", Content: commitPrompt + "\n\n<diff>\n" + diff + "\n</diff>"},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/0xhckr/gojo")
	req.Header.Set("X-Title", "gojo")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openrouter request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Try to extract a readable error message.
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
			return "", fmt.Errorf("openrouter %d: %s", resp.StatusCode, errResp.Error.Message)
		}
		return "", fmt.Errorf("openrouter %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("empty response from AI")
	}

	msg := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	// Strip any markdown fencing the model might add.
	msg = strings.TrimPrefix(msg, "```")
	msg = strings.TrimPrefix(msg, "git commit -m ")
	msg = strings.TrimSuffix(msg, "```")
	msg = strings.Trim(msg, "`\"\n")

	return msg, nil
}
