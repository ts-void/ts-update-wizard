package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Step struct {
	Title   string
	Command string
	Done    bool
}

type model struct {
	pkgName string
	steps   []Step
	cursor  int
}

var (
	titleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("99")).Bold(true)
	doneStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Strikethrough(true)
	normalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2).BorderForeground(lipgloss.Color("63"))
)

func initialModel(pkgName string) model {
	p := pkgName
	if p == "" {
		p = "<pkgname>"
	}

	steps := []Step{
		{Title: "Fork & Clone void-packages", Command: "git clone --depth 1 https://github.com/void-linux/void-packages.git"},
		{Title: "Set Upstream & Branch", Command: fmt.Sprintf("git remote add upstream https://github.com/void-linux/void-packages.git\ngit fetch upstream master --depth 1\ngit checkout -b %s-update", p)},
		{Title: "Bootstrap Environment", Command: "./xbps-src binary-bootstrap"},
		{Title: "Check for Updates", Command: fmt.Sprintf("./xbps-src update-check %s", p)},
		{Title: "Modify Template", Command: fmt.Sprintf("nano srcpkgs/%s/template\n# Set version, reset revision to 1", p)},
		{Title: "Generate Checksum", Command: fmt.Sprintf("xgensum -f -i %s", p)},
		{Title: "Build and Test", Command: fmt.Sprintf("./xbps-src pkg %s\n./xbps-src -Q pkg %s\n./xbps-src -a armv6l pkg %s", p, p, p)},
		{Title: "Install & Test on System", Command: fmt.Sprintf("xi -S %s\nxbps-query -p pkgver %s", p, p)},
		{Title: "Lint Package", Command: fmt.Sprintf("xlint %s", p)},
		{Title: "Check Upstream Changes", Command: fmt.Sprintf("# git clone <repo> /tmp/repo\n# cd /tmp/repo && git diff --stat <old> <new>")},
		{Title: "Commit (xbump)", Command: fmt.Sprintf("xbump %s", p)},
		{Title: "Push Branch", Command: fmt.Sprintf("git push -u origin %s-update", p)},
		{Title: "Create PR on GitHub", Command: fmt.Sprintf("# Title: %s: update to <version>", p)},
	}

	return model{
		pkgName: p,
		steps:   steps,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.steps)-1 {
				m.cursor++
			}
		case " ", "enter":
			m.steps[m.cursor].Done = !m.steps[m.cursor].Done
		}
	}
	return m, nil
}

func (m model) View() string {
	var sb strings.Builder

	sb.WriteString(titleStyle.Render(fmt.Sprintf(" Void Linux Package Update Wizard: [%s]\n", m.pkgName)) + "\n")

	for i, step := range m.steps {
		cursor := "  "
		if m.cursor == i {
			cursor = cursorStyle.Render("> ")
		}

		box := "[ ]"
		if step.Done {
			box = "[x]"
		}

		titleStr := step.Title
		if step.Done {
			titleStr = doneStyle.Render(titleStr)
		} else {
			titleStr = normalStyle.Render(titleStr)
		}

		sb.WriteString(fmt.Sprintf("%s%s %s\n", cursor, box, titleStr))
	}

	sb.WriteString("\n" + boxStyle.Render(fmt.Sprintf("Befehl für aktuellen Schritt:\n\n%s", m.steps[m.cursor].Command)) + "\n\n")
	sb.WriteString(helpStyle.Render("[↑/↓] Navigieren  [Space/Enter] Abhaken  [q] Beenden"))

	return sb.String()
}

func main() {
	pkg := ""
	if len(os.Args) > 1 {
		pkg = os.Args[1]
	}

	p := tea.NewProgram(initialModel(pkg))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Fehler beim Starten der TUI: %v\n", err)
		os.Exit(1)
	}
}
