# void-package-update-helper

A simple terminal UI (TUI) written in Go to guide you step-by-step through contributing package updates to [void-packages](https://github.com/void-linux/void-packages).

## Features

- **Interactive Checklist:** Track your progress step-by-step from cloning to opening a PR.
- **Context-Aware Commands:** Automatically formats commands with your specified package name.
- **Keyboard Navigation:** Quick and lightweight navigation using standard keybindings.

## Roadmap

- **Run command:** Run command with shortcut `r` instead of just giving the command to copy.
- **Read existing template:** Read the existing template file and get information like: repo, current version, ...
- **Find better name:** Find a better name. _void-package-update-helper_ is to bad and too long.

## Requirements

- [Go](https://go.dev/) 1.18 or higher

## Installation & Running

1. Clone this repository:
```bash
git clone git@github.com:ts-void/void-package-update-helper.git
cd void-package-update-helper
```

2. Run directly
```
go run main.go <package-name>
```

3. (Optional) Build and install binary
```
go build -o void-package-update-helper main.go
./void-package-update-helper zsh-completions
```

## Controls

| Key | Action |
| --- | --- |
| `↑` / `k` | Move up |
| `↓` / `j` | Move down |
| `Space` / `Enter` | Toggle step completed |
| `q` / `Ctrl+C` | Quit wizard |

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.