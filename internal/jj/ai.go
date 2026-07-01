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

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// AIDescribe generates a commit message for rev's diff via any
// OpenAI-compatible chat-completions API.
func (r *Runner) AIDescribe(rev string) (string, error) {
	if r.cfg.AIAPIKey == "" {
		return "", errors.New("No AI API key configured. Add ai_api_key to ~/.config/gojo/gojo.toml")
	}

	diffText, err := r.Diff(rev)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(diffText) == "" {
		return "", errors.New("No diff available for this commit")
	}

	baseURL := r.cfg.AIBaseURL
	if baseURL == "" {
		baseURL = DefaultAIBaseURL
	}
	model := r.cfg.AIModel
	if model == "" {
		model = DefaultAIModel
	}
	prompt := r.cfg.CommitPrompt
	if prompt == "" {
		prompt = defaultCommitPrompt
	}

	reqBody, err := json.Marshal(chatRequest{
		Model:     model,
		Messages:  []chatMessage{{Role: "user", Content: prompt + diffText}},
		MaxTokens: 200,
	})
	if err != nil {
		return "", err
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.cfg.AIAPIKey)

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
		return "", fmt.Errorf("AI API error (%d): %s", resp.StatusCode, snippet)
	}

	var parsed chatResponse
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
