package jj

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultCommitPrompt = "Write a clear, concise commit message (subject line only, no body) for this diff. Reply with ONLY the commit message text, nothing else:\n\n"

type orMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type orRequest struct {
	Model     string      `json:"model"`
	Messages  []orMessage `json:"messages"`
	MaxTokens int         `json:"max_tokens"`
}

type orResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// AIDescribe generates a commit message for rev's diff via the OpenRouter API.
func (r *Runner) AIDescribe(rev string) (string, error) {
	if r.cfg.OpenRouterAPIKey == "" {
		return "", errors.New("No OpenRouter API key configured. Add openrouter_api_key to ~/.config/gojo/gojo.toml")
	}

	diffText, err := r.Diff(rev)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(diffText) == "" {
		return "", errors.New("No diff available for this commit")
	}

	model := r.cfg.OpenRouterModel
	if model == "" {
		model = "anthropic/claude-sonnet-4"
	}
	prompt := r.cfg.CommitPrompt
	if prompt == "" {
		prompt = defaultCommitPrompt
	}

	reqBody, err := json.Marshal(orRequest{
		Model:     model,
		Messages:  []orMessage{{Role: "user", Content: prompt + diffText}},
		MaxTokens: 200,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.cfg.OpenRouterAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return "", fmt.Errorf("OpenRouter API error (%d): %s", resp.StatusCode, snippet)
	}

	var parsed orResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("Empty response from AI")
	}
	msg := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if msg == "" {
		return "", errors.New("Empty response from AI")
	}
	return msg, nil
}
