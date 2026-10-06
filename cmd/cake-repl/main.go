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
	"github.com/charmbracelet/x/ansi"
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

// stringList collects repeated flag values in order so -add-dir can be given
// once per directory, matching cake's repeatable --add-dir flag.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// optionalString accepts an optional value after normalization. It tracks
// whether the flag was supplied so -fork can distinguish "fork latest" from
// the flag being absent.
type optionalString struct {
	set   bool
	value string
}

func (s *optionalString) String() string { return s.value }

func (s *optionalString) Set(v string) error {
	s.set = true
	s.value = v
	return nil
}

// IsBoolFlag lets the standard flag package accept a bare -fork. A separate
// normalization step still supports the optional UUID value.
func (s *optionalString) IsBoolFlag() bool { return true }

// normalizeForkArgs turns cake's optional --fork [<uuid>] syntax into the
// standard flag package's --fork=<uuid> form. A bare --fork becomes --fork=.
func normalizeForkArgs(args []string) []string {
	normalized := make([]string, 0, len(args))
	parsingFlags := true
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			parsingFlags = false
			normalized = append(normalized, arg)
			continue
		}
		if parsingFlags && (arg == "-fork" || arg == "--fork") {
			if i+1 < len(args) && args[i+1] != "--" && !strings.HasPrefix(args[i+1], "-") {
				normalized = append(normalized, arg+"="+args[i+1])
				i++
			} else {
				normalized = append(normalized, arg+"=")
			}
			continue
		}
		normalized = append(normalized, arg)
	}
	return normalized
}

// validateFlags checks the mutually exclusive / incompatible flag combinations
// and returns an error when validation fails. When showVersion is true it
// short-circuits and returns nil so the caller handles --version separately.
func validateFlags(showVersion bool, resume string, fork bool, forkID string, args []string, configPath string, noConfig bool) error {
	if showVersion {
		return nil
	}
	if len(args) > 0 {
		return fmt.Errorf("unexpected argument %q (prompts are entered inside the REPL)", args[0])
	}
	if fork && resume != "" {
		return fmt.Errorf("-fork and -resume are mutually exclusive")
	}
	if resume != "" && !app.IsSessionID(resume) {
		return fmt.Errorf("invalid -resume uuid: %s", resume)
	}
	if forkID != "" && !app.IsSessionID(forkID) {
		return fmt.Errorf("invalid -fork uuid: %s", forkID)
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
	resume := flag.String("resume", "", "resume a specific cake session and reload its visible history")
	var fork optionalString
	flag.Var(&fork, "fork", "fork the latest cake session, or the specified session UUID")
	noSession := flag.Bool("no-session", false, "do not save the cake session to disk")
	model := flag.String("model", "", "model name passed through to cake")
	profile := flag.String("profile", "", "behavior profile passed through to cake")
	tools := flag.String("tools", "", "comma-separated tool names passed through to cake (restricts the session to these tools)")
	noTools := flag.Bool("no-tools", false, "expose no tools to cake")
	var addDirs stringList
	flag.Var(&addDirs, "add-dir", "directory to add to cake's sandbox as read-only (repeatable)")
	var toolboxDirs stringList
	flag.Var(&toolboxDirs, "toolbox", "directory of user-defined cake tools (repeatable)")
	sandbox := flag.String("sandbox", "", "sandbox policy passed through to cake")
	noSkills := flag.Bool("no-skills", false, "disable all cake skills")
	skills := flag.String("skills", "", "comma-separated skill names passed through to cake")
	systemPrompt := flag.String("system-prompt", "", "path to a custom cake system prompt file")
	cwd := flag.String("cwd", "", "working directory to run cake from (default: current directory)")
	inline := flag.Bool("inline", false, "render inline: keep recent terminal history visible above the REPL instead of using the alternate screen")
	noColor := flag.Bool("no-color", false, "disable styling")
	debugLog := flag.String("debug-log", "", "write cake-repl debug output to this file")
	historyFile := flag.String("history-file", "", "path to persist prompt history across restarts (default: no persistence)")
	showVersion := flag.Bool("version", false, "print version and exit")
	configPath := flag.String("config", "", "path to config file (overrides default XDG and project-local config)")
	noConfig := flag.Bool("no-config", false, "skip loading config file")
	outputLimit := flag.Int("output-limit", 0, "truncate tool output after this many characters (0 = use internal default 2000)")
	maxTimelineItems := flag.Int("max-timeline-items", 0, "limit timeline to this many entries (0 = no limit)")
	toolColor := flag.Bool("tool-color", false, "keep ANSI color in tool output (default: strip it)")
	if err = flag.CommandLine.Parse(normalizeForkArgs(os.Args[1:])); err != nil {
		return err
	}

	if err = validateFlags(*showVersion, *resume, fork.set, fork.value, flag.Args(), *configPath, *noConfig); err != nil {
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
		Tools:            *tools,
		NoTools:          *noTools,
		AddDirs:          addDirs,
		ToolboxDirs:      toolboxDirs,
		Sandbox:          *sandbox,
		NoSkills:         *noSkills,
		Skills:           *skills,
		SystemPrompt:     *systemPrompt,
		Fork:             fork.set,
		ForkID:           fork.value,
		NoSession:        *noSession,
		HistoryFile:      *historyFile,
		OutputLimit:      *outputLimit,
		MaxTimelineItems: *maxTimelineItems,
		ToolColor:        *toolColor,
		Inline:           *inline,
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
		if !explicit["tool-color"] && cfgFile.ToolColor {
			cfg.ToolColor = true
		}
	}

	if *resume != "" {
		cfg.InitialMode = cake.RunResume
		cfg.ResumeID = *resume
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

	// Inline mode renders in the normal buffer so recent terminal history stays
	// visible above the REPL; the alternate screen restores the terminal on exit
	// but hides that history for the whole session (ADR 015).
	opts := []tea.ProgramOption{tea.WithMouseCellMotion()}
	if !*inline {
		opts = append(opts, tea.WithAltScreen())
	}
	p := tea.NewProgram(app.New(cfg), opts...)
	m, err := p.Run()
	mod, ok := m.(app.Model)
	if ok {
		// CancelRunning is a no-op unless a run was still in flight, which is
		// exactly the case for a quit that never reached the model.
		mod.CancelRunning()
		if *inline {
			// Inline mode leaves the timeline on the terminal; drop the composer
			// rows so the shell prompt is not preceded by a stale input box.
			eraseInlineComposer(mod.ComposerRows())
		}
	}
	// The title reset does not go through the model: a panic in Update or View
	// returns no model at all, and that exit must still drop a working marker.
	resetTerminalTitle(cfg.Cwd)
	if err != nil {
		return err
	}
	if ok {
		if sessionID, _ := mod.SessionData(); sessionID != "" {
			fmt.Fprintf(os.Stderr, "\nResume this session with:\n")
			fmt.Fprintf(os.Stderr, "cake-repl -resume %s\n", sessionID)
		}
	}
	return nil
}

// eraseInlineComposer clears the composer rows that inline mode leaves on the
// terminal once Bubble Tea returns. The renderer erases the status line (the
// last frame row) on its own, so removing the composer above it leaves the
// timeline region, the resume message, and the shell prompt adjacent. Writes
// only to a terminal, never into redirected output, matching
// resetTerminalTitle.
func eraseInlineComposer(rows int) {
	if rows <= 0 || !stdoutIsTerminal() {
		return
	}
	fmt.Fprint(os.Stdout, inlineComposerCleanup(rows))
}

// inlineComposerCleanup returns the sequence that erases the composer rows of
// an inline-mode exit: the cursor moves up to the composer's first row and
// everything from there down is erased.
func inlineComposerCleanup(rows int) string {
	if rows <= 0 {
		return ""
	}
	return ansi.CursorUp(rows) + ansi.EraseScreenBelow
}

// resetTerminalTitle writes the idle terminal title once the program has
// returned. The model cannot do it for exits it never observes: Bubble Tea
// returns on SIGINT or SIGTERM without running Update, and a panic in Update or
// View returns no model at all, so the model's own title command never runs on
// either path. Redirected stdout is left untouched, so the escape never lands
// in a file or a pipe.
func resetTerminalTitle(cwd string) {
	if !stdoutIsTerminal() {
		return
	}
	fmt.Fprint(os.Stdout, app.IdleTitleSequence(cwd))
}

// stdoutIsTerminal reports whether stdout is a character device, so terminal
// control sequences are never written into redirected output.
func stdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
