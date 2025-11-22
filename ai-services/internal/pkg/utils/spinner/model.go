package spinner

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	success = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	error   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	spin    = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
)

type (
	updateMsg string
	stopMsg   string
	failMsg   string
)

type model struct {
	spinner  spinner.Model
	message  string
	done     bool
	isError  bool
	finalMsg string
}

func newModel(msg string) model {
	s := spinner.New()
	s.Spinner = spinner.Line
	s.Style = spin

	return model{
		spinner: s,
		message: msg,
	}
}

func (m model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case updateMsg:
		m.message = string(msg)
		return m, nil

	case stopMsg:
		m.done = true
		m.finalMsg = string(msg)
		return m, tea.Quit

	case failMsg:
		m.done = true
		m.isError = true
		m.finalMsg = string(msg)
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.done {
		if m.isError {
			return error.Render("✖") + " " + m.finalMsg + "\n"
		}
		return success.Render("✔") + " " + m.finalMsg + "\n"
	}
	return spin.Render(m.spinner.View()) + " " + m.message
}
