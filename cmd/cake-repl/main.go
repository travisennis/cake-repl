// Command cake-repl is an interactive terminal REPL for the cake CLI. It
// spawns one cake process per prompt with --output-format stream-json and
// renders the event stream live.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/travisennis/cake-repl/internal/app"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
	"github.com/travisennis/cake-repl/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cake-repl:", err)
		os.Exit(1)
	}
}

func run() (err error) {
	cakeBin := flag.String("cake-bin", "cake", "cake executable to run")
	continueFlag := flag.Bool("continue", false, "continue cake's latest session on the first prompt")
	resume := flag.String("resume", "", "resume a specific cake session uuid on the first prompt")
	model := flag.String("model", "", "model name passed through to cake")
	profile := flag.String("profile", "", "behavior profile passed through to cake")
	cwd := flag.String("cwd", "", "working directory to run cake from (default: current directory)")
	noColor := flag.Bool("no-color", false, "disable styling")
	debugLog := flag.String("debug-log", "", "write cake-repl debug output to this file")
	historyFile := flag.String("history-file", "", "path to persist prompt history across restarts (default: no persistence)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Fprintln(os.Stdout, version.Binary)
		return nil
	}
	if flag.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q (prompts are entered inside the REPL)", flag.Arg(0))
	}
	if *continueFlag && *resume != "" {
		return fmt.Errorf("--continue and --resume are mutually exclusive")
	}
	if *resume != "" && !app.IsSessionID(*resume) {
		return fmt.Errorf("invalid --resume uuid: %s", *resume)
	}

	if *noColor {
		lipgloss.SetColorProfile(termenv.Ascii)
	}

	dir := *cwd
	if dir == "" {
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("determining working directory: %w", err)
		}
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving --cwd: %w", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("--cwd is not a directory: %s", dir)
	}

	cfg := app.Config{
		CakeBin:     *cakeBin,
		Cwd:         dir,
		Model:       *model,
		Profile:     *profile,
		HistoryFile: *historyFile,
	}
	switch {
	case *resume != "":
		cfg.InitialMode = cake.RunResume
		cfg.ResumeID = *resume
	case *continueFlag:
		cfg.InitialMode = cake.RunContinue
	}

	if *debugLog != "" {
		f, err := os.OpenFile(*debugLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return fmt.Errorf("opening --debug-log: %w", err)
		}
		defer func() {
			if closeErr := f.Close(); err == nil && closeErr != nil {
				err = fmt.Errorf("closing --debug-log: %w", closeErr)
			}
		}()
		cfg.DebugLog = f
	}

	p := tea.NewProgram(app.New(cfg), tea.WithAltScreen())
	m, err := p.Run()
	if err != nil {
		return err
	}
	if mod, ok := m.(app.Model); ok {
		if sessionID, _ := mod.SessionData(); sessionID != "" {
			home, _ := os.UserHomeDir()
			fmt.Fprintf(os.Stderr, "\ncake\n")
			fmt.Fprintf(os.Stderr, "session id: %s\n", ui.ShortID(sessionID))
			fmt.Fprintf(os.Stderr, "file: %s\n",
				filepath.Join(home, ".local", "cake", "sessions", sessionID))
		}
	}
	return nil
}
