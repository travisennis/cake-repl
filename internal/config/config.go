// Package config handles loading TOML config files for cake-repl defaults.
//
// Merge order is: hardcoded defaults < config file < CLI flags. Config files
// are optional; a missing file is silently skipped.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config represents the config file structure. Fields use TOML tags matching
// the expected key names in the file. Zero values mean "not set" in a file
// that was actually loaded.
//
// ToolColor is the exception: false is a meaningful value, not "unset", so an
// explicit `tool-color = false` can turn off an inherited true. Its presence is
// tracked in ToolColorSet, taken from the decoded file's TOML metadata rather
// than inferred from the value. Compaction.Enabled is the same kind of value and
// uses the same presence rule.
type Config struct {
	Model            string      `toml:"model"`
	Profile          string      `toml:"profile"`
	OutputLimit      int         `toml:"output-limit"`
	MaxTimelineItems int         `toml:"max-timeline-items"`
	CakeBin          string      `toml:"cake-bin"`
	ToolColor        bool        `toml:"tool-color"`
	ToolColorSet     bool        `toml:"-"`
	Compaction       Compaction  `toml:"compaction"`
	Suggestions      Suggestions `toml:"suggestions"`
}

// Compaction holds the opt-in between-turn context compaction settings in the
// `[compaction]` table. Enabled is false by default and is the only field whose
// false value is meaningful, so EnabledSet records whether the file actually
// supplied the key.
//
// MaxContextTokens and TriggerTokens select absolute mode when either is
// non-zero; both must then be positive and the trigger must be below the
// ceiling. Both are between-turn targets: the REPL checks them only after a
// completed turn, so a single invocation can grow past them before the next
// prompt. When both are zero, the legacy ThresholdPercent trigger applies.
type Compaction struct {
	Enabled        bool   `toml:"enabled"`
	EnabledSet     bool   `toml:"-"`
	CakeCompactBin string `toml:"cake-compact-bin"`
	// MaxContextTokens is the quality ceiling, not a compactor threshold. It is
	// a between-turn target, never a live cap, and is never passed to the
	// compactor.
	MaxContextTokens int `toml:"max-context-tokens"`
	// TriggerTokens is the absolute between-turn context line at which the REPL
	// compacts. It is checked only after a completed turn.
	TriggerTokens int `toml:"trigger-tokens"`

	ThresholdPercent int `toml:"threshold-percent"`
	KeepTokens       int `toml:"keep-tokens"`
}

// Suggestions holds the opt-in after-turn prompt-suggestion settings in the
// `[suggestions]` table. Enabled is false by default and is the only field
// whose false value is meaningful, so EnabledSet records whether the file
// actually supplied the key. CakeSuggestBin names the external suggester; the
// REPL only invokes it and never reads a session file itself.
type Suggestions struct {
	Enabled        bool   `toml:"enabled"`
	EnabledSet     bool   `toml:"-"`
	CakeSuggestBin string `toml:"cake-suggest-bin"`
}

const (
	// DefaultCakeCompactBin is the compactor executable resolved on PATH when
	// the config does not name one.
	DefaultCakeCompactBin = "cake-compact"
	// DefaultCakeSuggestBin is the prompt suggester executable resolved on PATH
	// when the config does not name one.
	DefaultCakeSuggestBin = "cake-suggest"
	// DefaultCompactionThresholdPercent is the fallback trigger percentage when
	// the absolute trigger fields are unset. The REPL fires five points below it
	// after a completed turn.
	DefaultCompactionThresholdPercent = 80
	// DefaultCompactionKeepTokens is the retained-tail target; zero would let
	// the compactor apply its own window-relative default.
	DefaultCompactionKeepTokens = 20_000
)

// DefaultCompaction returns the effective compaction defaults before any config
// file is merged.
func DefaultCompaction() Compaction {
	return Compaction{
		CakeCompactBin:   DefaultCakeCompactBin,
		ThresholdPercent: DefaultCompactionThresholdPercent,
		KeepTokens:       DefaultCompactionKeepTokens,
	}
}

// DefaultSuggestions returns the effective suggestion defaults before any
// config file is merged. The feature stays disabled until trusted config
// enables it.
func DefaultSuggestions() Suggestions {
	return Suggestions{CakeSuggestBin: DefaultCakeSuggestBin}
}

// Validate checks the effective suggestion settings. Only an enabled feature
// needs an executable; a disabled table may omit it.
func (s Suggestions) Validate() error {
	if s.Enabled && s.CakeSuggestBin == "" {
		return errMissingSuggester
	}
	return nil
}

var errMissingSuggester = errors.New("suggestions.cake-suggest-bin must name an executable")

// Validate checks the effective compaction settings. Only values that cannot
// describe a trigger are rejected; zero means "use the default" and is resolved
// before validation, except that the two absolute fields must be set together.
// The compactor is validated only when it can run.
func (c Compaction) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.CakeCompactBin == "" {
		return errMissingCompactor
	}
	if c.MaxContextTokens < 0 {
		return fmt.Errorf("compaction.max-context-tokens must be greater than zero, got %d", c.MaxContextTokens)
	}
	if c.TriggerTokens < 0 {
		return fmt.Errorf("compaction.trigger-tokens must be greater than zero, got %d", c.TriggerTokens)
	}
	if (c.MaxContextTokens == 0) != (c.TriggerTokens == 0) {
		return errors.New("compaction.max-context-tokens and compaction.trigger-tokens must be set together")
	}
	if c.MaxContextTokens > 0 && c.TriggerTokens >= c.MaxContextTokens {
		return fmt.Errorf("compaction.trigger-tokens (%d) must be less than compaction.max-context-tokens (%d)", c.TriggerTokens, c.MaxContextTokens)
	}
	if c.ThresholdPercent < 1 || c.ThresholdPercent > 100 {
		return fmt.Errorf("compaction.threshold-percent must be between 1 and 100, got %d", c.ThresholdPercent)
	}
	if c.KeepTokens < 0 {
		return fmt.Errorf("compaction.keep-tokens must not be negative, got %d", c.KeepTokens)
	}
	return nil
}

var errMissingCompactor = errors.New("compaction.cake-compact-bin must name an executable")

// DefaultPaths returns the XDG config path and the project-local config path.
// The XDG path is $XDG_CONFIG_HOME/cake-repl/config.toml (falling back to
// ~/.config/cake-repl/config.toml). The project-local path is .cake-repl.toml
// in the current directory.
func DefaultPaths() (xdgPath, localPath string) {
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			xdg = filepath.Join(home, ".config")
		}
	}
	xdgPath = filepath.Join(xdg, "cake-repl", "config.toml")
	localPath = ".cake-repl.toml"
	return
}

// Load reads and decodes a TOML config file. A missing file returns an empty
// Config without error, so callers can always merge the result
// unconditionally. A file that exists but cannot be read or decoded (e.g.
// permission errors, invalid TOML) aborts with an error.
func Load(path string) (*Config, error) {
	cfg, _, err := load(path)
	return cfg, err
}

// LoadProject loads automatically discovered project defaults. A project file is
// untrusted provenance, so it cannot select an executable, cannot enable the
// metered, session-mutating compactor, and cannot tune its thresholds. ignored
// names each key dropped for that reason, in a stable order, and reports key
// presence even when its value is empty. A project file may still disable
// compaction. See ADR 006 and ADR 024.
func LoadProject(path string) (cfg *Config, ignored []string, err error) {
	cfg, meta, err := load(path)
	if err != nil {
		return nil, nil, err
	}
	if definedFold(meta, "cake-bin") {
		ignored = append(ignored, "cake-bin")
	}
	cfg.CakeBin = ""
	if definedFold(meta, "compaction", "cake-compact-bin") {
		ignored = append(ignored, "compaction.cake-compact-bin")
	}
	cfg.Compaction.CakeCompactBin = ""
	cfg.Suggestions.CakeSuggestBin = ""
	if definedFold(meta, "suggestions", "cake-suggest-bin") {
		ignored = append(ignored, "suggestions.cake-suggest-bin")
	}
	if definedFold(meta, "suggestions", "enabled") && cfg.Suggestions.Enabled {
		// Disabling is allowed to win; enabling is not.
		ignored = append(ignored, "suggestions.enabled")
		cfg.Suggestions.Enabled = false
		cfg.Suggestions.EnabledSet = false
	}
	if definedFold(meta, "compaction", "enabled") && cfg.Compaction.Enabled {
		// Disabling is allowed to win; enabling is not.
		ignored = append(ignored, "compaction.enabled")
		cfg.Compaction.Enabled = false
		cfg.Compaction.EnabledSet = false
	}
	// Tuning is ignored too: an untrusted file must not be able to turn an
	// already-enabled metered operation up. Clearing the value lets the trusted
	// layer's non-zero merge win. See ADR 024 and ADR 025.
	if definedFold(meta, "compaction", "max-context-tokens") {
		ignored = append(ignored, "compaction.max-context-tokens")
		cfg.Compaction.MaxContextTokens = 0
	}
	if definedFold(meta, "compaction", "trigger-tokens") {
		ignored = append(ignored, "compaction.trigger-tokens")
		cfg.Compaction.TriggerTokens = 0
	}
	if definedFold(meta, "compaction", "threshold-percent") {
		ignored = append(ignored, "compaction.threshold-percent")
		cfg.Compaction.ThresholdPercent = 0
	}
	if definedFold(meta, "compaction", "keep-tokens") {
		ignored = append(ignored, "compaction.keep-tokens")
		cfg.Compaction.KeepTokens = 0
	}
	return cfg, ignored, nil
}

func load(path string) (*Config, toml.MetaData, error) {
	cfg := &Config{}
	meta, err := toml.DecodeFile(path, cfg)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, meta, nil
		}
		return nil, meta, err
	}
	cfg.ToolColorSet = definedFold(meta, "tool-color")
	cfg.Compaction.EnabledSet = definedFold(meta, "compaction", "enabled")
	cfg.Suggestions.EnabledSet = definedFold(meta, "suggestions", "enabled")
	return cfg, meta, nil
}

// definedFold reports whether a TOML key path was supplied, matching each
// segment case-insensitively. The decoder matches struct tags case-insensitively,
// but MetaData.IsDefined is case-sensitive; presence checks must use the same
// rule as decoding, or a differently spelled key could decode into the struct
// while its presence check missed it. That matters most for the untrusted
// project-local layer, where a missed key would survive provenance stripping.
func definedFold(meta toml.MetaData, path ...string) bool {
	for _, key := range meta.Keys() {
		if len(key) != len(path) {
			continue
		}
		match := true
		for i := range path {
			if !strings.EqualFold(key[i], path[i]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// Merge applies src values to dst for each non-zero field in src. dst is
// modified in place and returned.
func Merge(dst, src *Config) *Config {
	if src == nil {
		return dst
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.Profile != "" {
		dst.Profile = src.Profile
	}
	if src.OutputLimit != 0 {
		dst.OutputLimit = src.OutputLimit
	}
	if src.MaxTimelineItems != 0 {
		dst.MaxTimelineItems = src.MaxTimelineItems
	}
	if src.CakeBin != "" {
		dst.CakeBin = src.CakeBin
	}
	// tool-color is a boolean whose false value is meaningful, so it merges on
	// key presence rather than on a non-zero value. Only a file that actually
	// supplied the key changes the effective value; a missing key inherits dst.
	if src.ToolColorSet {
		dst.ToolColor = src.ToolColor
		dst.ToolColorSet = true
	}
	mergeCompaction(&dst.Compaction, src.Compaction)
	mergeSuggestions(&dst.Suggestions, src.Suggestions)
	return dst
}

// mergeCompaction applies a higher-priority compaction table. Non-zero values
// override; enabled merges on key presence so an explicit false can disable an
// inherited true.
func mergeCompaction(dst *Compaction, src Compaction) {
	if src.EnabledSet {
		dst.Enabled = src.Enabled
		dst.EnabledSet = true
	}
	if src.CakeCompactBin != "" {
		dst.CakeCompactBin = src.CakeCompactBin
	}
	if src.MaxContextTokens != 0 {
		dst.MaxContextTokens = src.MaxContextTokens
	}
	if src.TriggerTokens != 0 {
		dst.TriggerTokens = src.TriggerTokens
	}
	if src.ThresholdPercent != 0 {
		dst.ThresholdPercent = src.ThresholdPercent
	}
	if src.KeepTokens != 0 {
		dst.KeepTokens = src.KeepTokens
	}
}

// MergeCompaction applies a config file's `[compaction]` table to a base
// carrying the hardcoded defaults. A nil file leaves the defaults untouched.
func MergeCompaction(base Compaction, file *Config) Compaction {
	if file != nil {
		mergeCompaction(&base, file.Compaction)
	}
	return base
}

// mergeSuggestions applies a higher-priority `[suggestions]` table.
// Enabled merges on key presence so an explicit false can disable an
// inherited true; the executable merges only when non-empty.
func mergeSuggestions(dst *Suggestions, src Suggestions) {
	if src.EnabledSet {
		dst.Enabled = src.Enabled
		dst.EnabledSet = true
	}
	if src.CakeSuggestBin != "" {
		dst.CakeSuggestBin = src.CakeSuggestBin
	}
}

// MergeSuggestions applies a config file's `[suggestions]` table to a base
// carrying the hardcoded defaults. A nil file leaves the defaults untouched.
func MergeSuggestions(base Suggestions, file *Config) Suggestions {
	if file != nil {
		mergeSuggestions(&base, file.Suggestions)
	}
	return base
}
