package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Header bar style: white bold on black background
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("0"))

	// State indicator styles
	activeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("220")) // Yellow

	waitingStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("46")) // Green

	idleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")) // Dim gray

	// Source styles
	cliSourceStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("51")) // Cyan

	ideSourceStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("51")) // Bold cyan

	// Column header style
	columnHeaderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("243")) // Gray

	// Help text style
	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")) // Dim gray

	// Dim style for idle rows
	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	// Normal text style for active/waiting rows
	normalTextStyle = lipgloss.NewStyle()

	// Cursor / selected row highlight
	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212")) // Pink highlight

	// Detail view label style
	detailLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("243"))

	// Detail view value style
	detailValueStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15"))

	// Filter prompt style
	filterPromptStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("214")) // Orange
)
