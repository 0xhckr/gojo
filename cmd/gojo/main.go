package main

import (
	"fmt"
	"os"

	"github.com/0xhckr/gojo/internal/ai"
	"github.com/0xhckr/gojo/internal/config"
	"github.com/0xhckr/gojo/internal/jj"
	"github.com/0xhckr/gojo/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	runner := jj.NewRunner(cfg.JJPath, cfg.RepoRoot)

	// Create AI client if OpenRouter key is configured.
	var aiClient *ai.Client
	if cfg.OpenRouterAPIKey != "" {
		model := cfg.OpenRouterModel
		if model == "" {
			model = "google/gemini-2.0-flash-001"
		}
		aiClient = ai.NewClient(cfg.OpenRouterAPIKey, model, cfg.CommitPrompt)
	}

	model := ui.NewModel(runner, aiClient)

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
