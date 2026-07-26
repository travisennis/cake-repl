// Package config handles loading TOML config files for cake-repl defaults.
//
// Merge order is: hardcoded defaults < config file < CLI flags. Config files
// are optional; a missing file is silently skipped.
package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config represents the config file structure. Fields use TOML tags matching
// the expected key names in the file. Zero values mean "not set" in a file
// that was actually loaded.
type Config struct {
	Model            string `toml:"model"`
	Profile          string `toml:"profile"`
	OutputLimit      int    `toml:"output-limit"`
	MaxTimelineItems int    `toml:"max-timeline-items"`
	CakeBin          string `toml:"cake-bin"`
}

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
	cfg := &Config{}
	_, err := toml.DecodeFile(path, cfg)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	return cfg, nil
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
	return dst
}
