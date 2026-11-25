package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/travisennis/cake-repl/internal/app"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/childproc"
	"github.com/travisennis/cake-repl/internal/config"
	"github.com/travisennis/cake-repl/internal/version"
)

const defaultSandbox = "workspace-write-interactive"

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

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

func (s *optionalString) IsBoolFlag() bool { return true }

var optionalValueFlags = []string{"fork", "resume"}

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

func resolveExecutablePath(path, base string) string {
	if path == "" || filepath.IsAbs(path) || !strings.ContainsRune(path, filepath.Separator) {
		return path
	}
	return filepath.Join(base, path)
}

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
	f.showVersion = fs.Bool("version", false, "print version, embedded revision, and commit time and exit")
	f.configPath = fs.String("config", "", "path to config file (overrides default XDG and project-local config)")
	f.noConfig = fs.Bool("no-config", false, "skip loading config file")
	f.outputLimit = fs.Int("output-limit", 0, "truncate tool output after this many bytes, cut on rune boundaries (0 = use internal default 2000)")
	f.maxTimelineItems = fs.Int("max-timeline-items", 0, "limit timeline to this many entries (0 = no limit)")
	f.toolColor = fs.Bool("tool-color", false, "keep ANSI color in tool output (default: strip it)")
	return f
}

func explicitFlags(fs *flag.FlagSet) map[string]bool {
	explicit := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	return explicit
}

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
		RestoreCwd:       f.resume.set && f.resume.value != "" && !explicit["cwd"],
		Explicit:         explicit,
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

	compaction := config.MergeCompaction(config.DefaultCompaction(), cfgFile)
	if validateErr := compaction.Validate(); validateErr != nil {
		return app.Config{}, validateErr
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
		Model:            compaction.Model,
		Profile:          compaction.Profile,
		ReasoningEffort:  compaction.ReasoningEffort,
	}

	suggestions := config.MergeSuggestions(config.DefaultSuggestions(), cfgFile)
	if validateErr := suggestions.Validate(); validateErr != nil {
		return app.Config{}, validateErr
	}
	if *f.noSession && suggestions.Enabled {
		fmt.Fprintf(warnings, "cake-repl: warning: prompt suggestions are disabled by -no-session\n")
		suggestions.Enabled = false
	}
	cfg.Suggestions = app.SuggestionConfig{
		Enabled:         suggestions.Enabled,
		Bin:             suggestions.CakeSuggestBin,
		Model:           suggestions.Model,
		Profile:         suggestions.Profile,
		ReasoningEffort: suggestions.ReasoningEffort,
	}

	watchCfg := config.MergeWatch(config.DefaultWatch(), cfgFile)
	if validateErr := watchCfg.Validate(); validateErr != nil {
		return app.Config{}, validateErr
	}
	interval := time.Second
	if watchCfg.Enabled {
		var err error
		interval, err = watchCfg.IntervalDuration()
		if err != nil {
			return app.Config{}, err
		}
	}
	sources := make([]app.WatchSource, 0, len(watchCfg.Sources))
	for _, source := range watchCfg.Sources {
		sources = append(sources, app.WatchSource{
			Name:    source.Name,
			Path:    source.Path,
			Pattern: source.Pattern,
			Prompt:  source.Prompt,
		})
	}
	cfg.Watch = app.WatchConfig{
		Enabled:  watchCfg.Enabled,
		Interval: interval,
		Sources:  sources,
	}

	cfg.CakeBin = resolveExecutablePath(cfg.CakeBin, dir)
	cfg.Compaction.Bin = resolveExecutablePath(cfg.Compaction.Bin, dir)
	cfg.Suggestions.Bin = resolveExecutablePath(cfg.Suggestions.Bin, dir)

	if f.resume.set {
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
	args := normalizeOptionalArgs(os.Args[1:])
	if err = flag.CommandLine.Parse(args); err != nil {
		return err
	}

	if err = validateFlags(*f.showVersion, f.resume, f.fork, flag.Args(), *f.configPath, *f.noConfig); err != nil {
		return err
	}
	if *f.showVersion {
		fmt.Fprintln(os.Stdout, version.Text())
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
	if err = freezeRelativeDataDir(cfg.Cwd); err != nil {
		return err
	}
	if !explicit["config"] && !*f.noConfig {
		cfg.ReloadProjectHelpers = projectHelpers
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

	logf := suspendLogf(cfg.DebugLog)
	runs := cake.NewRegistry(logf)
	cfg.Runs = runs
	children := childproc.NewRegistry()
	cfg.Children = children

	opts := []tea.ProgramOption{tea.WithMouseCellMotion()}
	if !*f.inline {
		opts = append(opts, tea.WithAltScreen())
	}
	p := tea.NewProgram(app.New(cfg), opts...)
	if stdoutIsTerminal() {
		stopSuspend := watchSuspend(p, *f.inline, runs, logf)
		defer stopSuspend()
	}
	m, err := p.Run()
	mod, ok, shutdownErr := shutdownAfterProgram(m, children)
	if shutdownErr != nil {
		if cfg.DebugLog != nil {
			fmt.Fprintf(cfg.DebugLog, "repl: %v\n", shutdownErr)
		}
		if err == nil {
			err = shutdownErr
		} else {
			fmt.Fprintln(os.Stderr, "cake-repl: shutdown:", shutdownErr)
		}
	}
	if ok {
		if *f.inline {
			eraseInlineComposer(mod.ComposerRows())
		}
	}
	resetTerminalTitle(cfg.Cwd)
	if err != nil {
		return err
	}
	if ok {
		if sessionID, _ := mod.SessionData(); sessionID != "" {
			fmt.Fprintf(os.Stderr, "\nResume this session with:\n")
			fmt.Fprintln(os.Stderr, resumeCommand(args, cfg.Cwd, sessionID))
		}
	}
	return nil
}

func shutdownAfterProgram(m tea.Model, children *childproc.Registry) (app.Model, bool, error) {
	mod, ok := m.(app.Model)
	if ok {
		return mod, true, mod.CancelRunning()
	}
	return app.Model{}, false, children.Shutdown(app.ChildTeardownTimeout)
}

func suspendLogf(w io.Writer) func(string, ...any) {
	return func(format string, args ...any) {
		if w != nil {
			fmt.Fprintf(w, format+"\n", args...)
		}
	}
}

func freezeRelativeDataDir(cwd string) error {
	value, ok := os.LookupEnv("CAKE_DATA_DIR")
	if !ok || value == "" || filepath.IsAbs(value) {
		return nil
	}
	resolved, err := filepath.Abs(filepath.Join(cwd, value))
	if err != nil {
		return fmt.Errorf("resolving CAKE_DATA_DIR: %w", err)
	}
	return os.Setenv("CAKE_DATA_DIR", resolved)
}

func projectHelpers(cwd string) (app.ProjectHelpers, error) {
	xdgPath, _ := config.DefaultPaths()
	xdgCfg, err := config.Load(xdgPath)
	if err != nil {
		return app.ProjectHelpers{}, fmt.Errorf("loading XDG config %s: %w", xdgPath, err)
	}
	localCfg, _, err := config.LoadProject(filepath.Join(cwd, ".cake-repl.toml"))
	if err != nil {
		return app.ProjectHelpers{}, fmt.Errorf("loading project-local config: %w", err)
	}
	merged := config.Merge(xdgCfg, localCfg)
	compaction := config.MergeCompaction(config.DefaultCompaction(), merged)
	suggestions := config.MergeSuggestions(config.DefaultSuggestions(), merged)
	watches := config.MergeWatch(config.DefaultWatch(), merged)
	var validationErrors []error
	if err := compaction.Validate(); err != nil {
		validationErrors = append(validationErrors, fmt.Errorf("compaction: %w", err))
	}
	if err := suggestions.Validate(); err != nil {
		validationErrors = append(validationErrors, fmt.Errorf("suggestions: %w", err))
	}
	if err := watches.Validate(); err != nil {
		validationErrors = append(validationErrors, fmt.Errorf("watch: %w", err))
	}
	if err := errors.Join(validationErrors...); err != nil {
		return app.ProjectHelpers{}, fmt.Errorf("validating project helper settings: %w", err)
	}
	return app.ProjectHelpers{
		CompactionEnabled:  compaction.Enabled,
		SuggestionsEnabled: suggestions.Enabled,
		WatchEnabled:       watches.Enabled,
	}, nil
}

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

func projectConfigWarning(key, path string) string {
	switch key {
	case "cake-bin":
		return fmt.Sprintf("cake-repl: warning: ignoring cake-bin in project-local config %q; use XDG config, -config, or -cake-bin to select the executable", path)
	case "compaction.cake-compact-bin":
		return fmt.Sprintf("cake-repl: warning: ignoring compaction.cake-compact-bin in project-local config %q; use XDG config or -config to select the compactor", path)
	case "suggestions.cake-suggest-bin":
		return fmt.Sprintf("cake-repl: warning: ignoring suggestions.cake-suggest-bin in project-local config %q; use XDG config or -config to select the suggester", path)
	case "suggestions.model":
		return fmt.Sprintf("cake-repl: warning: ignoring suggestions.model in project-local config %q; tune suggestions in XDG config or an explicit -config", path)
	case "suggestions.profile":
		return fmt.Sprintf("cake-repl: warning: ignoring suggestions.profile in project-local config %q; tune suggestions in XDG config or an explicit -config", path)
	case "suggestions.reasoning-effort":
		return fmt.Sprintf("cake-repl: warning: ignoring suggestions.reasoning-effort in project-local config %q; tune suggestions in XDG config or an explicit -config", path)
	case "suggestions.enabled":
		return fmt.Sprintf("cake-repl: warning: ignoring suggestions.enabled = true in project-local config %q; enable suggestions in XDG config or an explicit -config", path)
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
	case "compaction.model":
		return fmt.Sprintf("cake-repl: warning: ignoring compaction.model in project-local config %q; tune compaction in XDG config or an explicit -config", path)
	case "compaction.profile":
		return fmt.Sprintf("cake-repl: warning: ignoring compaction.profile in project-local config %q; tune compaction in XDG config or an explicit -config", path)
	case "compaction.reasoning-effort":
		return fmt.Sprintf("cake-repl: warning: ignoring compaction.reasoning-effort in project-local config %q; tune compaction in XDG config or an explicit -config", path)
	case "watch.enabled":
		return fmt.Sprintf("cake-repl: warning: ignoring watch.enabled = true in project-local config %q; enable path watches in XDG config or an explicit -config", path)
	case "watch.interval":
		return fmt.Sprintf("cake-repl: warning: ignoring watch.interval in project-local config %q; tune path watches in XDG config or an explicit -config", path)
	case "watch.sources":
		return fmt.Sprintf("cake-repl: warning: ignoring watch.sources in project-local config %q; define path watches in XDG config or an explicit -config", path)
	default:
		return fmt.Sprintf("cake-repl: warning: ignoring %s in project-local config %q", key, path)
	}
}

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

var resumeHintDisplayFlags = map[string]bool{
	"inline":             true,
	"no-color":           true,
	"tool-color":         true,
	"version":            true,
	"debug-log":          true,
	"history-file":       true,
	"output-limit":       true,
	"max-timeline-items": true,
}

func resumeCommand(args []string, cwd, sessionID string) string {
	parts := []string{"cake-repl"}
	cwdExplicit := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			parts = append(parts, shellQuote(arg))
			continue
		}
		name, inlineValue := resumeFlagName(arg)
		switch {
		case name == "resume" || name == "fork":
			continue
		case name == "cwd":
			cwdExplicit = true
			if !inlineValue && i+1 < len(args) {
				i++
			}
			continue
		case resumeHintDisplayFlags[name]:
			if !inlineValue && resumeFlagTakesValue(name) && i+1 < len(args) {
				i++
			}
			continue
		}
		parts = append(parts, shellQuote(arg))
		if !inlineValue && resumeFlagTakesValue(name) && i+1 < len(args) {
			i++
			parts = append(parts, shellQuote(args[i]))
		}
	}
	if cwdExplicit {
		parts = append(parts, "-cwd", shellQuote(cwd))
	}
	parts = append(parts, "-resume", shellQuote(sessionID))
	return strings.Join(parts, " ")
}

func resumeFlagName(arg string) (string, bool) {
	name := strings.TrimLeft(arg, "-")
	if i := strings.IndexByte(name, '='); i >= 0 {
		return name[:i], true
	}
	return name, false
}

func resumeFlagTakesValue(name string) bool {
	switch name {
	case "no-session", "no-tools", "no-skills", "inline", "no-color", "version", "no-config", "tool-color":
		return false
	default:
		return true
	}
}

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

func eraseInlineComposer(rows int) {
	if rows <= 0 || !stdoutIsTerminal() {
		return
	}
	fmt.Fprint(os.Stdout, inlineComposerCleanup(rows))
}

func inlineComposerCleanup(rows int) string {
	if rows <= 0 {
		return ""
	}
	return ansi.CursorUp(rows) + ansi.EraseScreenBelow
}

func resetTerminalTitle(cwd string) {
	if !stdoutIsTerminal() {
		return
	}
	fmt.Fprint(os.Stdout, app.IdleTitleSequence(cwd))
}

func stdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
