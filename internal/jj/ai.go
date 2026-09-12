package jj

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// aiHTTPClient is shared across AI describe calls so connections are pooled,
// and carries a timeout so a wedged endpoint can't hang a generation request
// (and its spinner) forever.
var aiHTTPClient = &http.Client{Timeout: 90 * time.Second}

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
			Content   string `json:"content"`
			Reasoning string `json:"reasoning"`
		} `json:"message"`
	} `json:"choices"`
}

// AIDescribe generates a commit message for rev's diff using the configured
// provider. Applying the message remains the caller's responsibility.
func (r *Runner) AIDescribe(rev string) (string, error) {
	switch r.cfg.AIProvider {
	case "", "api":
		if r.cfg.AIAPIKey == "" {
			return "", errors.New("No AI API key configured. Add ai_api_key or set ai_provider = \"codex\" in ~/.config/gojo/gojo.toml")
		}
	case "codex":
	default:
		return "", fmt.Errorf("Unknown AI provider %q: use api or codex", r.cfg.AIProvider)
	}

	diffText, err := r.Diff(rev)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(diffText) == "" {
		return "", errors.New("No diff available for this commit")
	}
	prompt := r.cfg.CommitPrompt
	if prompt == "" {
		prompt = defaultCommitPrompt
	}
	prompt += diffText

	if r.cfg.AIProvider == "codex" {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		return r.codexDescribe(ctx, prompt)
	}
	return r.apiDescribe(prompt)
}

func (r *Runner) apiDescribe(prompt string) (string, error) {
	baseURL := r.cfg.AIBaseURL
	if baseURL == "" {
		baseURL = DefaultAIBaseURL
	}
	model := r.cfg.AIModel
	if model == "" {
		model = DefaultAIModel
	}
	reqBody, err := json.Marshal(chatRequest{
		Model:     model,
		Messages:  []chatMessage{{Role: "user", Content: prompt}},
		MaxTokens: 2048,
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

	resp, err := aiHTTPClient.Do(req)
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
		msg = strings.TrimSpace(parsed.Choices[0].Message.Reasoning)
	}
	if msg == "" {
		return "", errors.New("Empty response from AI")
	}
	return msg, nil
}
