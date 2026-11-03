package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Config structure for config.json
type Config struct {
	VoidPackagesPath string `json:"void_packages_path"`
}

type Step struct {
	Title   string
	Command string
	Done    bool
}

type model struct {
	pkgName        string
	config         Config
	currentVersion string
	steps          []Step
	cursor         int
	outputLog      string
	running        bool
	message        string
	activeChan     chan tea.Msg
}

var (
	titleStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	cursorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("99")).Bold(true)
	doneStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Strikethrough(true)
	normalStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	helpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	boxStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 2).BorderForeground(lipgloss.Color("63"))
	outputBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 2).BorderForeground(lipgloss.Color("39"))
	statusBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 2).BorderForeground(lipgloss.Color("214"))
)

type outputLineMsg string
type execFinishedMsg struct {
	err error
}

// Load config file or fall back to default value
func loadConfig() Config {
	defaultPath := "./void-packages"
	file, err := os.Open("config.json")
	if err != nil {
		return Config{VoidPackagesPath: defaultPath}
	}
	defer file.Close()

	var cfg Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil || cfg.VoidPackagesPath == "" {
		cfg.VoidPackagesPath = defaultPath
	}
	return cfg
}

// Reads the current version from the void-packages template if available
func parseTemplateVersion(cfg Config, pkgName string) string {
	templatePath := filepath.Join(cfg.VoidPackagesPath, "srcpkgs", pkgName, "template")

	file, err := os.Open(templatePath)
	if err != nil {
		return "unknown"
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "version=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.Trim(parts[1], `"'`)
			}
		}
	}

	return "unknown"
}

func initialModel(pkgName string) model {
	p := pkgName
	if p == "" {
		p = "<pkgname>"
	}

	cfg := loadConfig()
	detectedVersion := parseTemplateVersion(cfg, p)
	repoDir := cfg.VoidPackagesPath

	steps := []Step{
		{Title: "Clean local branches", Command: "git branch | grep -v \"^\\*\" | xargs git branch -D"},
		{Title: "Fork & Clone void-packages", Command: fmt.Sprintf("git clone --depth 1 https://github.com/void-linux/void-packages.git %s", repoDir)},
		{Title: "Set Upstream & Branch", Command: fmt.Sprintf("cd %s && git remote add upstream https://github.com/void-linux/void-packages.git && git fetch upstream master --depth 1 && git checkout -b %s-update", repoDir, p)},
		{Title: "Binary Bootstrap", Command: fmt.Sprintf("cd %s && ./xbps-src binary-bootstrap", repoDir)},
		{Title: "Check for Updates", Command: fmt.Sprintf("cd %s && ./xbps-src update-check %s", repoDir, p)},
		{Title: "Modify Template", Command: fmt.Sprintf("nano %s", filepath.Join(repoDir, "srcpkgs", p, "template"))},
		{Title: "Generate Checksum", Command: fmt.Sprintf("cd %s && xgensum -f -i %s", repoDir, p)},
		{Title: "Build (Native x86_64)", Command: fmt.Sprintf("cd %s && ./xbps-src pkg %s", repoDir, p)},
		{Title: "Build (aarch64)", Command: fmt.Sprintf("cd %s && ./xbps-src -a aarch64 pkg %s", repoDir, p)},
		{Title: "Build (aarch64-musl)", Command: fmt.Sprintf("cd %s && ./xbps-src -a aarch64-musl pkg %s", repoDir, p)},
		{Title: "Build (armv7l)", Command: fmt.Sprintf("cd %s && ./xbps-src -a armv7l pkg %s", repoDir, p)},
		{Title: "Build (armv6l)", Command: fmt.Sprintf("cd %s && ./xbps-src -a armv6l pkg %s", repoDir, p)},
		{Title: "Build (i686)", Command: fmt.Sprintf("cd %s && ./xbps-src -a i686 pkg %s", repoDir, p)},
		{Title: "Build (riscv64)", Command: fmt.Sprintf("cd %s && ./xbps-src -a riscv64 pkg %s", repoDir, p)},
		{Title: "Install & Test on System", Command: fmt.Sprintf("cd %s && xi -S %s", repoDir, p)},
		{Title: "Lint Package", Command: fmt.Sprintf("cd %s && xlint %s", repoDir, p)},
		{Title: "Commit (xbump)", Command: fmt.Sprintf("cd %s && xbump %s", repoDir, p)},
		{Title: "Push Branch", Command: fmt.Sprintf("cd %s && git push -u origin %s-update", repoDir, p)},
	}

	return model{
		pkgName:        p,
		config:         cfg,
		currentVersion: detectedVersion,
		steps:          steps,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

// Background worker that executes command and pushes lines into buffered channel
func runCommandAndStream(command string, ch chan tea.Msg) {
	cmd := exec.Command("bash", "-c", command)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		ch <- execFinishedMsg{err: err}
		close(ch)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		ch <- execFinishedMsg{err: err}
		close(ch)
		return
	}

	if err := cmd.Start(); err != nil {
		ch <- execFinishedMsg{err: err}
		close(ch)
		return
	}

	scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
	for scanner.Scan() {
		ch <- outputLineMsg(scanner.Text() + "\n")
	}

	err = cmd.Wait()
	ch <- execFinishedMsg{err: err}
	close(ch)
}

// Continuously polls the channel for new lines without blocking the TUI event loop
func waitForData(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.running {
			return m, nil
		}
		m.message = ""
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
		case "r":
			m.running = true
			m.outputLog = ""
			m.message = "Running..."
			m.activeChan = make(chan tea.Msg, 256) // Buffered channel to prevent blocking
			go runCommandAndStream(m.steps[m.cursor].Command, m.activeChan)
			return m, waitForData(m.activeChan)
		}

	case outputLineMsg:
		m.outputLog += string(msg)
		lines := strings.Split(m.outputLog, "\n")
		if len(lines) > 12 {
			m.outputLog = strings.Join(lines[len(lines)-12:], "\n")
		}
		// Recursively wait for the next output line or completion message
		return m, waitForData(m.activeChan)

	case execFinishedMsg:
		m.running = false
		if msg.err != nil {
			m.message = fmt.Sprintf("Execution Error: %v", msg.err)
		} else {
			m.message = "Command executed successfully!"
			m.steps[m.cursor].Done = true

			if m.cursor == 1 || m.cursor == 5 {
				m.currentVersion = parseTemplateVersion(m.config, m.pkgName)
			}
		}
		return m, nil
	}
	return m, nil
}

func (m model) View() string {
	var sb strings.Builder

	header := fmt.Sprintf(" Void Linux Package Update Wizard: [%s] (Version: %s)\n", m.pkgName, m.currentVersion)
	sb.WriteString(titleStyle.Render(header))
	sb.WriteString("\n")

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

	sb.WriteString("\n")
	sb.WriteString(boxStyle.Render(fmt.Sprintf("Command: %s", m.steps[m.cursor].Command)))
	sb.WriteString("\n\n")

	logDisplay := m.outputLog
	if logDisplay == "" {
		logDisplay = "(No output yet / ready)"
	}
	sb.WriteString(outputBoxStyle.Render(fmt.Sprintf("Live Output:\n%s", logDisplay)))
	sb.WriteString("\n\n")

	statusText := m.message
	if statusText == "" {
		statusText = "Status: Ready"
	}
	sb.WriteString(statusBoxStyle.Render(statusText))
	sb.WriteString("\n\n")

	sb.WriteString(helpStyle.Render("[↑/↓] Navigate  [Space/Enter] Toggle  [r] Run  [q] Quit"))

	return sb.String()
}

func main() {
	pkg := ""
	if len(os.Args) > 1 {
		pkg = os.Args[1]
	}

	p := tea.NewProgram(initialModel(pkg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
