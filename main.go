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

type Step struct {
	Title   string `json:"title"`
	Command string `json:"command"`
	Done    bool   `json:"-"`
}

// Config structure for config.json (including git_username, editor and tasks)
type Config struct {
	VoidPackagesPath string `json:"void_packages_path"`
	GitUsername      string `json:"git_username"`
	Editor           string `json:"editor"`
	Tasks            []Step `json:"tasks"`
}

type model struct {
	pkgName        string
	config         Config
	currentVersion string
	updateStatus   string
	steps          []Step
	cursor         int
	outputLog      string
	running        bool
	message        string
	activeChan     chan tea.Msg
	width          int
	height         int
}

var (
	titleStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	versionStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("246")).Italic(true)
	cursorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("99")).Bold(true)
	doneStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Strikethrough(true)
	normalStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	helpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	boxStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 2).BorderForeground(lipgloss.Color("63"))
	outputBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 2).BorderForeground(lipgloss.Color("39"))
	statusBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 2).BorderForeground(lipgloss.Color("214"))
)

type outputLineMsg string
type updateCheckFinishedMsg struct {
	output string
	err    error
}
type execFinishedMsg struct {
	err error
}

// Load config file and parse tasks with variable substitution
func loadConfig(pkgName string) Config {
	defaultPath := "./void-packages"
	file, err := os.Open("config.json")
	if err != nil {
		return Config{
			VoidPackagesPath: defaultPath,
			GitUsername:      "your-username",
			Editor:           "nano",
			Tasks: []Step{
				{Title: "Binary Bootstrap", Command: fmt.Sprintf("cd %s && ./xbps-src binary-bootstrap", defaultPath)},
			},
		}
	}
	defer file.Close()

	var cfg Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil || cfg.VoidPackagesPath == "" {
		cfg.VoidPackagesPath = defaultPath
	}
	if cfg.GitUsername == "" {
		cfg.GitUsername = "your-username"
	}

	// Determine editor: config.json -> $EDITOR -> fallback (nano)
	editor := cfg.Editor
	if editor == "" {
		editor = os.Getenv("EDITOR")
		if editor == "" {
			editor = "nano"
		}
	}

	// Fallback if no tasks defined in json
	if len(cfg.Tasks) == 0 {
		cfg.Tasks = []Step{
			{Title: "Binary Bootstrap", Command: fmt.Sprintf("cd %s && ./xbps-src binary-bootstrap", cfg.VoidPackagesPath)},
		}
	}

	// Replace placeholders ({{repo}}, {{pkg}}, {{git_username}}, and {{editor}}) in all tasks
	for i := range cfg.Tasks {
		cmd := cfg.Tasks[i].Command
		cmd = strings.ReplaceAll(cmd, "{{repo}}", cfg.VoidPackagesPath)
		cmd = strings.ReplaceAll(cmd, "{{pkg}}", pkgName)
		cmd = strings.ReplaceAll(cmd, "{{git_username}}", cfg.GitUsername)
		cmd = strings.ReplaceAll(cmd, "{{editor}}", editor)
		cfg.Tasks[i].Command = cmd
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

	if scanErr := scanner.Err(); scanErr != nil {
		return "unknown"
	}

	return "unknown"
}

func initialModel(pkgName string) model {
	p := pkgName
	if p == "" {
		p = "<pkgname>"
	}

	cfg := loadConfig(p)
	detectedVersion := parseTemplateVersion(cfg, p)

	return model{
		pkgName:        p,
		config:         cfg,
		currentVersion: detectedVersion,
		updateStatus:   "Checking...",
		steps:          cfg.Tasks,
		width:          80,
	}
}

// Automatically check for updates on startup in the background
func checkUpdatesCmd(cfg Config, pkgName string) tea.Cmd {
	return func() tea.Msg {
		repoDir := cfg.VoidPackagesPath
		if _, err := os.Stat(repoDir); os.IsNotExist(err) {
			return updateCheckFinishedMsg{output: "", err: fmt.Errorf("repo not cloned")}
		}
		cmdStr := fmt.Sprintf("cd %s && ./xbps-src update-check %s", repoDir, pkgName)
		cmd := exec.Command("bash", "-c", cmdStr)
		cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
		outBytes, err := cmd.CombinedOutput()
		return updateCheckFinishedMsg{output: string(outBytes), err: err}
	}
}

func (m model) Init() tea.Cmd {
	return checkUpdatesCmd(m.config, m.pkgName)
}

// Background worker that executes command and pushes lines into buffered channel
func runCommandAndStream(command string, ch chan tea.Msg) {
	cmd := exec.Command("bash", "-c", command)
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")

	r, w := io.Pipe()
	cmd.Stdout = w
	cmd.Stderr = w

	if err := cmd.Start(); err != nil {
		w.Close()
		ch <- execFinishedMsg{err: err}
		close(ch)
		return
	}

	go func() {
		err := cmd.Wait()
		w.Close()
		ch <- execFinishedMsg{err: err}
	}()

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		ch <- outputLineMsg(scanner.Text() + "\n")
	}

	if scanErr := scanner.Err(); scanErr != nil {
		ch <- outputLineMsg(fmt.Sprintf("[Scanner Error: %v]\n", scanErr))
	}

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
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case updateCheckFinishedMsg:
		out := strings.TrimSpace(msg.output)
		if msg.err != nil || out == "" {
			m.updateStatus = "Up to date"
		} else {
			lines := strings.Split(out, "\n")
			m.updateStatus = strings.TrimSpace(lines[len(lines)-1])
		}
		return m, nil

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
			cmdStr := m.steps[m.cursor].Command

			// Prüfen, ob der Befehl interaktiv sein muss (Editor, xi-Installationen oder sudo)
			isInteractive := strings.Contains(cmdStr, "nano") || strings.Contains(cmdStr, "hx") ||
				strings.Contains(cmdStr, "vim") || strings.Contains(cmdStr, "vi") ||
				strings.Contains(cmdStr, "micro") || strings.Contains(cmdStr, "emacs") ||
				strings.Contains(cmdStr, "xi ") || strings.Contains(cmdStr, "sudo")

			if isInteractive {
				// Terminal-Kontrolle kurz abgeben, damit sudo/xi Passwörter sauber abfragen kann
				c := exec.Command("bash", "-c", cmdStr)
				return m, tea.ExecProcess(c, func(err error) tea.Msg {
					return execFinishedMsg{err: err}
				})
			} else {
				// Nicht-interaktive Befehle im Hintergrund mit Live-Log-Stream ausführen
				m.running = true
				m.outputLog = ""
				m.message = "Running..."
				m.activeChan = make(chan tea.Msg, 256)
				go runCommandAndStream(cmdStr, m.activeChan)
				return m, waitForData(m.activeChan)
			}
		}

	case outputLineMsg:
		m.outputLog += string(msg)
		lines := strings.Split(m.outputLog, "\n")
		if len(lines) > 12 {
			m.outputLog = strings.Join(lines[len(lines)-12:], "\n")
		}
		return m, waitForData(m.activeChan)

	case execFinishedMsg:
		m.running = false
		if msg.err != nil {
			m.message = fmt.Sprintf("Execution Error: %v", msg.err)
		} else {
			m.message = "Command executed successfully!"
			m.steps[m.cursor].Done = true

			// Template-Version nach Änderungen automatisch aktualisieren
			m.currentVersion = parseTemplateVersion(m.config, m.pkgName)
		}
		return m, nil
	}
	return m, nil
}

func (m model) View() string {
	var sb strings.Builder

	boxWidth := m.width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}

	titleLine := fmt.Sprintf(" Void Linux Package Update Wizard: [%s]", m.pkgName)
	sb.WriteString(titleStyle.Render(titleLine))
	sb.WriteString("\n")

	versionDisplay := m.currentVersion
	if m.updateStatus != "" && m.updateStatus != "Checking..." {
		versionDisplay = fmt.Sprintf("%s (%s)", m.currentVersion, m.updateStatus)
	}
	versionLine := fmt.Sprintf(" Version: %s\n", versionDisplay)
	sb.WriteString(versionStyle.Render(versionLine))
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

	styledCmdBox := boxStyle.Width(boxWidth).Render(fmt.Sprintf("Command: %s", m.steps[m.cursor].Command))
	sb.WriteString(styledCmdBox)
	sb.WriteString("\n\n")

	logDisplay := m.outputLog
	if logDisplay == "" {
		logDisplay = "(No output yet / ready)"
	}
	styledOutputBox := outputBoxStyle.Width(boxWidth).Render(fmt.Sprintf("Live Output:\n%s", logDisplay))
	sb.WriteString(styledOutputBox)
	sb.WriteString("\n\n")

	statusText := m.message
	if statusText == "" {
		statusText = "Status: Ready"
	}
	styledStatusBox := statusBoxStyle.Width(boxWidth).Render(statusText)
	sb.WriteString(styledStatusBox)
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
