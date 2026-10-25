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

// defaultSandbox is the sandbox policy cake-repl passes to cake unless the user
// overrides it. It is deliberately not cake's own default (workspace-write): a
// REPL is interactive, and workspace-write-interactive adds the macOS
// capabilities that let a command launch and automate other applications. See
// ADR 017.
const defaultSandbox = "workspace-write-interactive"

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
// whether the flag was supplied so -fork and -resume can distinguish a bare
// flag from the flag being absent.
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

// IsBoolFlag lets the standard flag package accept a bare optional-value flag.
// A separate normalization step still supports the optional UUID value.
func (s *optionalString) IsBoolFlag() bool { return true }

// optionalValueFlags names the startup flags whose value may be omitted: the
// flag package has no optional-value form, so normalizeOptionalArgs rewrites
// these before Parse.
var optionalValueFlags = []string{"fork", "resume"}

// isOptionalValueFlag reports whether arg names one of the optional-value flags
// in bare form ("-fork", "--resume", and so on). Every other spelling — the
// -flag=value form the flag package already accepts, another flag, or a
// positional argument — is left for Parse.
func isOptionalValueFlag(arg string) bool {
	name, ok := strings.CutPrefix(arg, "-")
	if !ok {
		return false
	}
	name, _ = strings.CutPrefix(name, "-")
	for _, candidate := range optionalValueFlags {
		if name == candidate {
			return true
		}
	}
	return false
}

// normalizeOptionalArgs turns an optional-value flag's `--flag [<uuid>]` syntax
// into the standard flag package's `--flag=<uuid>` form. A bare --fork or
// --resume becomes --flag=, which the bound optionalString records as supplied
// with no value.
func normalizeOptionalArgs(args []string) []string {
	normalized := make([]string, 0, len(args))
	parsingFlags := true
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			parsingFlags = false
			normalized = append(normalized, arg)
			continue
		}
		if parsingFlags && isOptionalValueFlag(arg) {
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
//
// Both optional-value flags are mutually exclusive in every form, so a bare
// -resume and a bare -fork are rejected together exactly like their
// UUID-bearing forms; only a value that was supplied must be a UUID, because a
// bare -resume opens the session picker instead of naming a session (ADR 021).
func validateFlags(showVersion bool, resume, fork optionalString, args []string, configPath string, noConfig bool) error {
	if showVersion {
		return nil
	}
	if len(args) > 0 {
		return fmt.Errorf("unexpected argument %q (prompts are entered inside the REPL)", args[0])
	}
	if fork.set && resume.set {
		return fmt.Errorf("-fork and -resume are mutually exclusive")
	}
	if resume.value != "" && !app.IsSessionID(resume.value) {
		return fmt.Errorf("invalid -resume uuid: %s", resume.value)
	}
	if fork.value != "" && !app.IsSessionID(fork.value) {
		return fmt.Errorf("invalid -fork uuid: %s", fork.value)
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

// startupFlags holds the bound values of every startup flag. bindFlags registers
// them on fs and returns the holder, so the process command line and the
// exit-hint round-trip tests parse one shared flag surface.
type startupFlags struct {
	cakeBin          *string
	resume           optionalString
	fork             optionalString
	noSession        *bool
	model            *string
	profile          *string
	tools            *string
	noTools          *bool
	addDirs          stringList
	toolboxDirs      stringList
	sandbox          *string
	noSkills         *bool
	skills           *string
	systemPrompt     *string
	cwd              *string
	inline           *bool
	noColor          *bool
	debugLog         *string
	historyFile      *string
	showVersion      *bool
	configPath       *string
	noConfig         *bool
	outputLimit      *int
	maxTimelineItems *int
	toolColor        *bool
}

// bindFlags registers the startup flag surface on fs. Flag names, defaults, and
// help text are user-facing; keep them aligned with README.md and the CLI/output
// guardrail.
func bindFlags(fs *flag.FlagSet) *startupFlags {
	f := &startupFlags{}
	f.cakeBin = fs.String("cake-bin", "cake", "cake executable to run")
	fs.Var(&f.resume, "resume", "resume a specific cake session and reload its visible history, or pick one at startup when no UUID is given")
	fs.Var(&f.fork, "fork", "fork the latest cake session, or the specified session UUID")
	f.noSession = fs.Bool("no-session", false, "do not save the cake session to disk")
	f.model = fs.String("model", "", "model name passed through to cake")
	f.profile = fs.String("profile", "", "behavior profile passed through to cake")
	f.tools = fs.String("tools", "", "comma-separated tool names passed through to cake (restricts the session to these tools)")
	f.noTools = fs.Bool("no-tools", false, "expose no tools to cake")
	fs.Var(&f.addDirs, "add-dir", "directory to add to cake's sandbox as read-only (repeatable)")
	fs.Var(&f.toolboxDirs, "toolbox", "directory of user-defined cake tools (repeatable)")
	f.sandbox = fs.String("sandbox", defaultSandbox, "sandbox policy passed through to cake")
	f.noSkills = fs.Bool("no-skills", false, "disable all cake skills")
	f.skills = fs.String("skills", "", "comma-separated skill names passed through to cake")
	f.systemPrompt = fs.String("system-prompt", "", "path to a custom cake system prompt file")
	f.cwd = fs.String("cwd", "", "working directory to run cake from (default: current directory)")
	f.inline = fs.Bool("inline", false, "render inline: keep recent terminal history visible above the REPL instead of using the alternate screen")
	f.noColor = fs.Bool("no-color", false, "disable styling")
	f.debugLog = fs.String("debug-log", "", "write cake-repl debug output to this file")
	f.historyFile = fs.String("history-file", "", "path to persist prompt history across restarts (default: no persistence)")
	f.showVersion = fs.Bool("version", false, "print version and exit")
	f.configPath = fs.String("config", "", "path to config file (overrides default XDG and project-local config)")
	f.noConfig = fs.Bool("no-config", false, "skip loading config file")
	f.outputLimit = fs.Int("output-limit", 0, "truncate tool output after this many bytes, cut on rune boundaries (0 = use internal default 2000)")
	f.maxTimelineItems = fs.Int("max-timeline-items", 0, "limit timeline to this many entries (0 = no limit)")
	f.toolColor = fs.Bool("tool-color", false, "keep ANSI color in tool output (default: strip it)")
	return f
}

// explicitFlags reports which flags the user set on the command line, so a
// config-file value applies only where the flag was omitted, and the exit hint
// can re-emit an explicit override whose effective value is empty or the
// default.
func explicitFlags(fs *flag.FlagSet) map[string]bool {
	explicit := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	return explicit
}

// effectiveConfig resolves bound flags and config-file defaults into the
// effective app.Config. The merge order is hardcoded defaults < config file <
// CLI flags; explicit names the flags the user set, and warnings receives any
// startup advisory.
func effectiveConfig(f *startupFlags, explicit map[string]bool, warnings io.Writer) (app.Config, error) {
	dir, err := resolveCwd(*f.cwd)
	if err != nil {
		return app.Config{}, err
	}
	cfgFile, err := loadStartupConfig(*f.configPath, explicit["config"], *f.noConfig, warnings)
	if err != nil {
		return app.Config{}, err
	}
	cfg := app.Config{
		CakeBin:          *f.cakeBin,
		Cwd:              dir,
		Model:            *f.model,
		Profile:          *f.profile,
		Tools:            *f.tools,
		NoTools:          *f.noTools,
		AddDirs:          f.addDirs,
		ToolboxDirs:      f.toolboxDirs,
		Sandbox:          *f.sandbox,
		NoSkills:         *f.noSkills,
		Skills:           *f.skills,
		SystemPrompt:     *f.systemPrompt,
		Fork:             f.fork.set,
		ForkID:           f.fork.value,
		NoSession:        *f.noSession,
		HistoryFile:      *f.historyFile,
		OutputLimit:      *f.outputLimit,
		MaxTimelineItems: *f.maxTimelineItems,
		ToolColor:        *f.toolColor,
		Inline:           *f.inline,
	}
	applyConfigFile(&cfg, cfgFile, explicit)

	// Resolve the `[compaction]` table over the hardcoded defaults. There is no
	// CLI flag for it, so the config file (or its absence) is authoritative.
	compaction := config.MergeCompaction(config.DefaultCompaction(), cfgFile)
	if err := compaction.Validate(); err != nil {
		return app.Config{}, err
	}
	if *f.noSession && compaction.Enabled {
		fmt.Fprintf(warnings, "cake-repl: warning: context compaction is disabled by -no-session\n")
		compaction.Enabled = false
	}
	cfg.Compaction = app.CompactionConfig{
		Enabled:          compaction.Enabled,
		Bin:              compaction.CakeCompactBin,
		MaxContextTokens: compaction.MaxContextTokens,
		TriggerTokens:    compaction.TriggerTokens,
		ThresholdPercent: compaction.ThresholdPercent,
		KeepTokens:       compaction.KeepTokens,
	}

	if f.resume.set {
		// A bare -resume names no session, so nothing is hydrated and nothing is
		// pinned: the REPL opens the session picker and the picked row pins the
		// next prompt through the /resume transition. See ADR 021.
		if f.resume.value == "" {
			cfg.StartupPicker = true
		} else {
			cfg.InitialMode = cake.RunResume
			cfg.ResumeID = f.resume.value
		}
	}
	return cfg, nil
}

func run() (err error) {
	f := bindFlags(flag.CommandLine)
	if err = flag.CommandLine.Parse(normalizeOptionalArgs(os.Args[1:])); err != nil {
		return err
	}

	if err = validateFlags(*f.showVersion, f.resume, f.fork, flag.Args(), *f.configPath, *f.noConfig); err != nil {
		return err
	}
	if *f.showVersion {
		fmt.Fprintln(os.Stdout, version.Binary)
		return nil
	}

	explicit := explicitFlags(flag.CommandLine)

	if *f.noColor {
		lipgloss.SetColorProfile(termenv.Ascii)
	}

	cfg, err := effectiveConfig(f, explicit, os.Stderr)
	if err != nil {
		return err
	}

	if *f.debugLog != "" {
		var logFile *os.File
		logFile, err = os.OpenFile(*f.debugLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return fmt.Errorf("opening -debug-log: %w", err)
		}
		defer func() {
			if closeErr := logFile.Close(); err == nil && closeErr != nil {
				err = fmt.Errorf("closing -debug-log: %w", closeErr)
			}
		}()
		cfg.DebugLog = &syncWriter{w: logFile}
	}

	// Inline mode renders in the normal buffer so recent terminal history stays
	// visible above the REPL; the alternate screen restores the terminal on exit
	// but hides that history for the whole session (ADR 015).
	opts := []tea.ProgramOption{tea.WithMouseCellMotion()}
	if !*f.inline {
		opts = append(opts, tea.WithAltScreen())
	}
	p := tea.NewProgram(app.New(cfg), opts...)
	// A catchable external SIGTSTP stops this process without giving the
	// terminal back, so the shell's fg resumes a REPL that still believes it
	// owns raw mode. Release the terminal before stopping and re-own it after
	// SIGCONT; only meaningful when this process owns a terminal. See ADR 018.
	if stdoutIsTerminal() {
		stopSuspend := watchSuspend(p, *f.inline, suspendLogf(cfg.DebugLog))
		defer stopSuspend()
	}
	m, err := p.Run()
	mod, ok := m.(app.Model)
	if ok {
		// CancelRunning is a no-op unless a run was still in flight, which is
		// exactly the case for a quit that never reached the model.
		mod.CancelRunning()
		if *f.inline {
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
			fmt.Fprintln(os.Stderr, resumeCommand(cfg, *f.configPath, *f.noConfig, sessionID, explicit))
		}
	}
	return nil
}

// terminalProgram is the slice of *tea.Program the suspend watcher drives. It is
// declared once here rather than in each build-tagged file so the two never
// drift. It is an interface so the suspend cycle's ordering can be exercised
// without a real program.
type terminalProgram interface {
	ReleaseTerminal() error
	RestoreTerminal() error
}

// suspendLogf returns the logger for terminal-lifecycle diagnostics. It writes
// to the -debug-log writer, or discards when none was configured. The debug log
// uses one `tag: message` line per event (`stdout:`, `exit:`), so callers pass a
// message that starts with the `suspend:` tag.
func suspendLogf(w io.Writer) func(string, ...any) {
	return func(format string, args ...any) {
		if w != nil {
			fmt.Fprintf(w, format+"\n", args...)
		}
	}
}

// loadStartupConfig enforces executable provenance at the project-local layer.
// Explicit files remain trusted, including an explicitly selected local file.
func loadStartupConfig(configPath string, explicitConfig, noConfig bool, warnings io.Writer) (*config.Config, error) {
	switch {
	case explicitConfig:
		cfg, err := config.Load(configPath)
		if err != nil {
			return nil, fmt.Errorf("loading config: %w", err)
		}
		return cfg, nil
	case noConfig:
		return nil, nil
	default:
		xdgPath, localPath := config.DefaultPaths()
		xdgCfg, err := config.Load(xdgPath)
		if err != nil {
			return nil, fmt.Errorf("loading XDG config %s: %w", xdgPath, err)
		}
		localCfg, ignored, err := config.LoadProject(localPath)
		if err != nil {
			return nil, fmt.Errorf("loading project-local config %s: %w", localPath, err)
		}
		for _, key := range ignored {
			fmt.Fprintln(warnings, projectConfigWarning(key, localPath))
		}
		return config.Merge(xdgCfg, localCfg), nil
	}
}

// projectConfigWarning explains why a key in the automatically loaded
// project-local config was dropped. The text never echoes the configured value.
func projectConfigWarning(key, path string) string {
	switch key {
	case "cake-bin":
		return fmt.Sprintf("cake-repl: warning: ignoring cake-bin in project-local config %q; use XDG config, -config, or -cake-bin to select the executable", path)
	case "compaction.cake-compact-bin":
		return fmt.Sprintf("cake-repl: warning: ignoring compaction.cake-compact-bin in project-local config %q; use XDG config or -config to select the compactor", path)
	case "compaction.enabled":
		return fmt.Sprintf("cake-repl: warning: ignoring compaction.enabled = true in project-local config %q; enable compaction in XDG config or an explicit -config", path)
	case "compaction.max-context-tokens":
		return fmt.Sprintf("cake-repl: warning: ignoring compaction.max-context-tokens in project-local config %q; tune compaction in XDG config or an explicit -config", path)
	case "compaction.trigger-tokens":
		return fmt.Sprintf("cake-repl: warning: ignoring compaction.trigger-tokens in project-local config %q; tune compaction in XDG config or an explicit -config", path)
	case "compaction.threshold-percent":
		return fmt.Sprintf("cake-repl: warning: ignoring compaction.threshold-percent in project-local config %q; tune compaction in XDG config or an explicit -config", path)
	case "compaction.keep-tokens":
		return fmt.Sprintf("cake-repl: warning: ignoring compaction.keep-tokens in project-local config %q; tune compaction in XDG config or an explicit -config", path)
	default:
		return fmt.Sprintf("cake-repl: warning: ignoring %s in project-local config %q", key, path)
	}
}

// applyConfigFile applies file defaults only where a CLI flag was not supplied.
func applyConfigFile(cfg *app.Config, cfgFile *config.Config, explicit map[string]bool) {
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
		if !explicit["tool-color"] && cfgFile.ToolColorSet {
			cfg.ToolColor = cfgFile.ToolColor
		}
	}
}

// resumeCommand renders a shell-ready command that resumes sessionID with the
// same startup controls this process ran with. Cake session controls such as
// -sandbox, -model, and -cwd shape every cake invocation and are not recorded
// anywhere cake exposes for replay, so a resume command that omitted them would
// silently change the resumed run's behavior (for example, reverting an
// explicitly selected read-only sandbox to the workspace-write-interactive
// default, or letting a config file re-supply a model the run was started
// without).
//
// explicit names the flags the user set at startup. An override whose effective
// value is empty (an explicit -model "" or -sandbox "") or equal to the default
// (an explicit -cake-bin cake) must be re-emitted, because reloading the config
// on resume would otherwise re-supply a value the original run suppressed.
func resumeCommand(cfg app.Config, configPath string, noConfig bool, sessionID string, explicit map[string]bool) string {
	parts := []string{"cake-repl", "-resume", shellQuote(sessionID)}
	// addFlag always emits the name and a shell-quoted value, even when the value
	// is empty, so an explicit empty override survives the paste; shellQuote
	// renders "" as ''.
	addFlag := func(name, value string) {
		parts = append(parts, name, shellQuote(value))
	}
	// addControl emits a value-taking flag when the user set it explicitly or
	// when its effective value is non-empty. A bare default stays out of the
	// hint, so an ordinary resume stays short and still reproduces the run.
	addControl := func(name, value string, explicitFlag bool) {
		if explicitFlag || value != "" {
			addFlag(name, value)
		}
	}
	// The default executable name is omitted unless it was an explicit override,
	// which must be re-emitted so a config file cannot re-select its own value.
	if explicit["cake-bin"] || (cfg.CakeBin != "" && cfg.CakeBin != "cake") {
		addFlag("-cake-bin", cfg.CakeBin)
	}
	// Config selection is part of the run: pasting a hint without -no-config
	// would reload a config file the original run skipped.
	if noConfig {
		parts = append(parts, "-no-config")
	} else {
		addControl("-config", configPath, explicit["config"])
	}
	addFlag("-cwd", cfg.Cwd)
	addControl("-model", cfg.Model, explicit["model"])
	addControl("-profile", cfg.Profile, explicit["profile"])
	addControl("-sandbox", cfg.Sandbox, explicit["sandbox"])
	for _, dir := range cfg.AddDirs {
		parts = append(parts, "-add-dir", shellQuote(dir))
	}
	for _, dir := range cfg.ToolboxDirs {
		parts = append(parts, "-toolbox", shellQuote(dir))
	}
	addControl("-tools", cfg.Tools, explicit["tools"])
	if cfg.NoTools {
		parts = append(parts, "-no-tools")
	}
	if cfg.NoSkills {
		parts = append(parts, "-no-skills")
	}
	addControl("-skills", cfg.Skills, explicit["skills"])
	addControl("-system-prompt", cfg.SystemPrompt, explicit["system-prompt"])
	return strings.Join(parts, " ")
}

// shellQuote wraps s in single quotes when it contains characters the shell
// would otherwise interpret, so a pasted resume command reproduces a path or
// value with spaces, globs, or metacharacters verbatim.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_', r == '-', r == '.', r == '/', r == ':', r == '@', r == '+', r == '=', r == ',', r == '%':
		default:
			safe = false
		}
		if !safe {
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
