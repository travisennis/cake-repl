package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

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

type Compaction struct {
	Enabled          bool   `toml:"enabled"`
	EnabledSet       bool   `toml:"-"`
	CakeCompactBin   string `toml:"cake-compact-bin"`
	MaxContextTokens int    `toml:"max-context-tokens"`
	TriggerTokens    int    `toml:"trigger-tokens"`

	ThresholdPercent int `toml:"threshold-percent"`
	KeepTokens       int `toml:"keep-tokens"`

	Model           string `toml:"model"`
	Profile         string `toml:"profile"`
	ReasoningEffort string `toml:"reasoning-effort"`
}

type Suggestions struct {
	Enabled         bool   `toml:"enabled"`
	EnabledSet      bool   `toml:"-"`
	CakeSuggestBin  string `toml:"cake-suggest-bin"`
	Model           string `toml:"model"`
	Profile         string `toml:"profile"`
	ReasoningEffort string `toml:"reasoning-effort"`
}

const (
	DefaultCakeCompactBin             = "cake-compact"
	DefaultCakeSuggestBin             = "cake-suggest"
	DefaultCompactionThresholdPercent = 80
	DefaultCompactionKeepTokens       = 20_000
)

func DefaultCompaction() Compaction {
	return Compaction{
		CakeCompactBin:   DefaultCakeCompactBin,
		ThresholdPercent: DefaultCompactionThresholdPercent,
		KeepTokens:       DefaultCompactionKeepTokens,
	}
}

func DefaultSuggestions() Suggestions {
	return Suggestions{CakeSuggestBin: DefaultCakeSuggestBin}
}

func (s Suggestions) Validate() error {
	if !s.Enabled {
		return nil
	}
	if s.CakeSuggestBin == "" {
		return errMissingSuggester
	}
	switch s.ReasoningEffort {
	case "", "none", "low", "medium", "high", "xhigh", "max":
	default:
		return fmt.Errorf("suggestions.reasoning-effort must be one of none, low, medium, high, xhigh, or max, got %q", s.ReasoningEffort)
	}
	return nil
}

var errMissingSuggester = errors.New("suggestions.cake-suggest-bin must name an executable")

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
	switch c.ReasoningEffort {
	case "", "none", "low", "medium", "high", "xhigh", "max":
	default:
		return fmt.Errorf("compaction.reasoning-effort must be one of none, low, medium, high, xhigh, or max, got %q", c.ReasoningEffort)
	}
	return nil
}

var errMissingCompactor = errors.New("compaction.cake-compact-bin must name an executable")

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

func Load(path string) (*Config, error) {
	cfg, _, err := load(path)
	return cfg, err
}

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
	if definedFold(meta, "suggestions", "model") {
		ignored = append(ignored, "suggestions.model")
		cfg.Suggestions.Model = ""
	}
	if definedFold(meta, "suggestions", "profile") {
		ignored = append(ignored, "suggestions.profile")
		cfg.Suggestions.Profile = ""
	}
	if definedFold(meta, "suggestions", "reasoning-effort") {
		ignored = append(ignored, "suggestions.reasoning-effort")
		cfg.Suggestions.ReasoningEffort = ""
	}
	if definedFold(meta, "suggestions", "enabled") && cfg.Suggestions.Enabled {
		ignored = append(ignored, "suggestions.enabled")
		cfg.Suggestions.Enabled = false
		cfg.Suggestions.EnabledSet = false
	}
	if definedFold(meta, "compaction", "enabled") && cfg.Compaction.Enabled {
		ignored = append(ignored, "compaction.enabled")
		cfg.Compaction.Enabled = false
		cfg.Compaction.EnabledSet = false
	}
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
	if definedFold(meta, "compaction", "model") {
		ignored = append(ignored, "compaction.model")
		cfg.Compaction.Model = ""
	}
	if definedFold(meta, "compaction", "profile") {
		ignored = append(ignored, "compaction.profile")
		cfg.Compaction.Profile = ""
	}
	if definedFold(meta, "compaction", "reasoning-effort") {
		ignored = append(ignored, "compaction.reasoning-effort")
		cfg.Compaction.ReasoningEffort = ""
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
	if src.ToolColorSet {
		dst.ToolColor = src.ToolColor
		dst.ToolColorSet = true
	}
	mergeCompaction(&dst.Compaction, src.Compaction)
	mergeSuggestions(&dst.Suggestions, src.Suggestions)
	return dst
}

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
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.Profile != "" {
		dst.Profile = src.Profile
	}
	if src.ReasoningEffort != "" {
		dst.ReasoningEffort = src.ReasoningEffort
	}
}

func MergeCompaction(base Compaction, file *Config) Compaction {
	if file != nil {
		mergeCompaction(&base, file.Compaction)
	}
	return base
}

func mergeSuggestions(dst *Suggestions, src Suggestions) {
	if src.EnabledSet {
		dst.Enabled = src.Enabled
		dst.EnabledSet = true
	}
	if src.CakeSuggestBin != "" {
		dst.CakeSuggestBin = src.CakeSuggestBin
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.Profile != "" {
		dst.Profile = src.Profile
	}
	if src.ReasoningEffort != "" {
		dst.ReasoningEffort = src.ReasoningEffort
	}
}

func MergeSuggestions(base Suggestions, file *Config) Suggestions {
	if file != nil {
		mergeSuggestions(&base, file.Suggestions)
	}
	return base
}
