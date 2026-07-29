// Command cake-repl is an interactive terminal REPL for the cake CLI. It
// spawns one cake process per prompt with --output-format stream-json and
// renders the event stream live.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/travisennis/cake-repl/internal/app"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/config"
	"github.com/travisennis/cake-repl/internal/version"
)

// syncWriter serializes writes to an underlying io.Writer. The debug log is
// written from both the cake runner goroutine and the Bubble Tea update
// goroutine, so the writer that reaches both paths must be safe for concurrent
// use rather than relying on the underlying *os.File behavior.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// sessionFilePath returns the path where cake stores the session file for the
// given session ID. It respects $CAKE_DATA_DIR when set, otherwise uses
// ~/.local/share/cake/sessions/<id>.jsonl.
func sessionFilePath(home, sessionID string) string {
	if d := os.Getenv("CAKE_DATA_DIR"); d != "" {
		return filepath.Join(d, "sessions", sessionID+".jsonl")
	}
	return filepath.Join(home, ".local", "share", "cake", "sessions", sessionID+".jsonl")
}

// displayPath replaces a leading home directory prefix with ~ for compact
// display. Paths not under home (e.g. a custom $CAKE_DATA_DIR) are returned
// unchanged.
func displayPath(home, path string) string {
	if home != "" && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// validateFlags checks the mutually exclusive / incompatible flag combinations
// and returns an error when validation fails. When showVersion is true it
// short-circuits and returns nil so the caller handles --version separately.
func validateFlags(showVersion bool, continueFlag bool, resume string, args []string, configPath string, noConfig bool) error {
	if showVersion {
		return nil
	}
	if len(args) > 0 {
		return fmt.Errorf("unexpected argument %q (prompts are entered inside the REPL)", args[0])
	}
	if continueFlag && resume != "" {
		return fmt.Errorf("-continue and -resume are mutually exclusive")
	}
	if resume != "" && !app.IsSessionID(resume) {
		return fmt.Errorf("invalid -resume uuid: %s", resume)
	}
	if configPath != "" && noConfig {
		return fmt.Errorf("-config and -no-config are mutually exclusive")
	}
	return nil
}

// resolveCwd resolves the -cwd flag value to an absolute directory path.
// When cwd is empty it falls back to os.Getwd. It errors when the path
// cannot be resolved or does not exist as a directory.
func resolveCwd(cwd string) (string, error) {
	dir := cwd
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("determining working directory: %w", err)
		}
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving -cwd: %w", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("-cwd is not a directory: %s", dir)
	}
	return dir, nil
}

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
	configPath := flag.String("config", "", "path to config file (overrides default XDG and project-local config)")
	noConfig := flag.Bool("no-config", false, "skip loading config file")
	outputLimit := flag.Int("output-limit", 0, "truncate tool output after this many characters (0 = use internal default 2000)")
	maxTimelineItems := flag.Int("max-timeline-items", 0, "limit timeline to this many entries (0 = no limit)")
	flag.Parse()

	if err = validateFlags(*showVersion, *continueFlag, *resume, flag.Args(), *configPath, *noConfig); err != nil {
		return err
	}
	if *showVersion {
		fmt.Fprintln(os.Stdout, version.Binary)
		return nil
	}

	// Collect explicitly-set flag names so config-file values only apply when
	// the user did not pass the corresponding flag. This implements the merge
	// order: hardcoded defaults < config file < CLI flags.
	explicit := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		explicit[f.Name] = true
	})

	if *noColor {
		lipgloss.SetColorProfile(termenv.Ascii)
	}

	var dir string
	dir, err = resolveCwd(*cwd)
	if err != nil {
		return err
	}

	// Load config file(s).
	var cfgFile *config.Config
	switch {
	case explicit["config"]:
		cfgFile, err = config.Load(*configPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
	case *noConfig:
		// Skip config loading entirely.
	default:
		xdgPath, localPath := config.DefaultPaths()
		xdgCfg, xdgErr := config.Load(xdgPath)
		if xdgErr != nil {
			return fmt.Errorf("loading XDG config %s: %w", xdgPath, xdgErr)
		}
		localCfg, localErr := config.Load(localPath)
		if localErr != nil {
			return fmt.Errorf("loading project-local config %s: %w", localPath, localErr)
		}
		cfgFile = config.Merge(xdgCfg, localCfg)
	}

	// Build the final app.Config: hardcoded defaults < config file < CLI flags.
	cfg := app.Config{
		CakeBin:          *cakeBin,
		Cwd:              dir,
		Model:            *model,
		Profile:          *profile,
		HistoryFile:      *historyFile,
		OutputLimit:      *outputLimit,
		MaxTimelineItems: *maxTimelineItems,
	}

	// Apply config file values for fields not explicitly set via CLI.
	if cfgFile != nil {
		if !explicit["model"] && cfgFile.Model != "" {
			cfg.Model = cfgFile.Model
		}
		if !explicit["profile"] && cfgFile.Profile != "" {
			cfg.Profile = cfgFile.Profile
		}
		if !explicit["cake-bin"] && cfgFile.CakeBin != "" {
			cfg.CakeBin = cfgFile.CakeBin
		}
		if !explicit["output-limit"] && cfgFile.OutputLimit != 0 {
			cfg.OutputLimit = cfgFile.OutputLimit
		}
		if !explicit["max-timeline-items"] && cfgFile.MaxTimelineItems != 0 {
			cfg.MaxTimelineItems = cfgFile.MaxTimelineItems
		}
	}

	switch {
	case *resume != "":
		cfg.InitialMode = cake.RunResume
		cfg.ResumeID = *resume
	case *continueFlag:
		cfg.InitialMode = cake.RunContinue
	}

	if *debugLog != "" {
		var f *os.File
		f, err = os.OpenFile(*debugLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return fmt.Errorf("opening -debug-log: %w", err)
		}
		defer func() {
			if closeErr := f.Close(); err == nil && closeErr != nil {
				err = fmt.Errorf("closing -debug-log: %w", closeErr)
			}
		}()
		cfg.DebugLog = &syncWriter{w: f}
	}

	p := tea.NewProgram(app.New(cfg), tea.WithAltScreen(), tea.WithMouseCellMotion())
	m, err := p.Run()
	if err != nil {
		if mod, ok := m.(app.Model); ok {
			mod.CancelRunning()
		}
		return err
	}
	if mod, ok := m.(app.Model); ok {
		if sessionID, _ := mod.SessionData(); sessionID != "" {
			home, _ := os.UserHomeDir()
			fmt.Fprintf(os.Stderr, "\nResume this session with:\n")
			fmt.Fprintf(os.Stderr, "cake-repl -resume %s\n", sessionID)
			fmt.Fprintf(os.Stderr, "file: %s\n",
				displayPath(home, sessionFilePath(home, sessionID)))
		}
	}
	return nil
}
