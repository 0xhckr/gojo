package ui

import "github.com/charmbracelet/lipgloss"

var (
	// Base colors.
	colorPurple     = lipgloss.Color("135")
	colorDarkPurple = lipgloss.Color("91")
	colorBlue       = lipgloss.Color("69")
	colorGreen      = lipgloss.Color("78")
	colorRed        = lipgloss.Color("167")
	colorYellow     = lipgloss.Color("179")
	colorGray       = lipgloss.Color("245")
	colorDarkGray   = lipgloss.Color("238")
	colorDarkerGray = lipgloss.Color("235")
	colorWhite      = lipgloss.Color("252")

	// Commit list styles.
	styleChangeID    = lipgloss.NewStyle().Foreground(colorPurple).Bold(true)
	styleCommitID    = lipgloss.NewStyle().Foreground(colorGray)
	styleSubject     = lipgloss.NewStyle().Foreground(colorWhite)
	styleAuthor      = lipgloss.NewStyle().Foreground(colorBlue)
	styleDate        = lipgloss.NewStyle().Foreground(colorGray)
	styleBookmark    = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	styleWorkingCopy = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	styleImmutable   = lipgloss.NewStyle().Foreground(colorDarkGray)
	styleCursor      = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	styleSelected    = lipgloss.NewStyle().Background(colorDarkPurple).Foreground(colorWhite)

	// Status styles.
	styleAdded    = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	styleModified = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	styleRemoved  = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	styleConflict = lipgloss.NewStyle().Foreground(colorRed).Bold(true).Blink(true)
	stylePath     = lipgloss.NewStyle().Foreground(colorWhite)

	// UI chrome.
	styleTitle = lipgloss.NewStyle().
			Foreground(colorWhite).
			Background(colorPurple).
			Padding(0, 1).
			Bold(true)
	styleHelpBar = lipgloss.NewStyle().
			Foreground(colorGray).
			Background(colorDarkerGray).
			Padding(0, 1)
	styleDiffHeader = lipgloss.NewStyle().Foreground(colorBlue).Bold(true)
	styleDiffAdd    = lipgloss.NewStyle().Foreground(colorGreen)
	styleDiffDel    = lipgloss.NewStyle().Foreground(colorRed)
	styleError      = lipgloss.NewStyle().Foreground(colorRed).Background(colorDarkerGray).Bold(true)
	styleMuted      = lipgloss.NewStyle().Foreground(colorGray).Background(colorDarkerGray)
)
