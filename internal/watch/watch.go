package watch

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Source struct {
	Name    string
	Path    string
	Pattern string
	Prompt  string
}

type Config struct {
	Sources []Source
}

type Event struct {
	Source string
	Path   string
	Files  []string
	Prompt string
}

type fileStamp struct {
	size    int64
	modTime time.Time
}

func (s fileStamp) equal(other fileStamp) bool {
	return s.size == other.size && s.modTime.Equal(other.modTime)
}

type sourceState struct {
	initialized bool
	path        string
	seen        map[string]fileStamp
}

type Tracker struct {
	sources []Source
	states  []sourceState
}

func New(cfg Config) *Tracker {
	sources := append([]Source(nil), cfg.Sources...)
	return &Tracker{
		sources: sources,
		states:  make([]sourceState, len(sources)),
	}
}

func (t *Tracker) Scan(sessionID, cwd string) []Event {
	if t == nil {
		return nil
	}
	var events []Event
	for i, source := range t.sources {
		path, ok := resolvePath(source.Path, sessionID, cwd)
		if !ok {
			continue
		}
		state := &t.states[i]
		current, err := readFiles(path, source.Pattern)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				state.initialized = true
				state.path = path
				state.seen = map[string]fileStamp{}
			}
			continue
		}
		if !state.initialized || state.path != path {
			state.initialized = true
			state.path = path
			state.seen = current
			continue
		}
		files := make([]string, 0)
		for name, stamp := range current {
			if previous, ok := state.seen[name]; !ok || !previous.equal(stamp) {
				files = append(files, name)
			}
		}
		state.seen = current
		if len(files) == 0 {
			continue
		}
		sort.Strings(files)
		events = append(events, Event{
			Source: source.Name,
			Path:   path,
			Files:  files,
			Prompt: source.Prompt,
		})
	}
	return events
}

func ExpandPrompt(prompt string, event Event, sessionID, cwd string) string {
	files := formatFiles(event.Files)
	replacer := strings.NewReplacer(
		"{source}", event.Source,
		"{path}", event.Path,
		"{count}", strconv.Itoa(len(event.Files)),
		"{files}", files,
		"{session_id}", sessionID,
		"{cwd}", cwd,
	)
	return replacer.Replace(prompt)
}

func readFiles(path, pattern string) (map[string]fileStamp, error) {
	if pattern == "" {
		pattern = "*"
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	files := make(map[string]fileStamp, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match, err := filepath.Match(pattern, entry.Name())
		if err != nil || !match {
			continue
		}
		var stamp fileStamp
		if info, err := entry.Info(); err == nil {
			stamp.size = info.Size()
			stamp.modTime = info.ModTime()
		}
		files[entry.Name()] = stamp
	}
	return files, nil
}

func resolvePath(template, sessionID, cwd string) (string, bool) {
	if strings.TrimSpace(template) == "" {
		return "", false
	}
	path, ok := expandEnv(template)
	if !ok {
		return "", false
	}
	if strings.Contains(path, "{session_id}") && sessionID == "" {
		return "", false
	}
	path = strings.ReplaceAll(path, "{session_id}", sessionID)
	path = strings.ReplaceAll(path, "{cwd}", cwd)
	switch {
	case path == "~":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		path = home
	case strings.HasPrefix(path, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	return filepath.Clean(path), true
}

func expandEnv(value string) (string, bool) {
	complete := true
	expanded := os.Expand(value, func(key string) string {
		replacement, ok := os.LookupEnv(key)
		if !ok {
			complete = false
			return ""
		}
		return replacement
	})
	return expanded, complete
}

func formatFiles(files []string) string {
	const limit = 10
	shown := files
	if len(shown) > limit {
		shown = shown[:limit]
	}
	names := make([]string, 0, len(shown))
	for _, name := range shown {
		names = append(names, shortName(filepath.Base(name)))
	}
	result := strings.Join(names, ", ")
	if remaining := len(files) - len(shown); remaining > 0 {
		result += fmt.Sprintf(", and %d more", remaining)
	}
	return result
}

func shortName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	const limit = 80
	if len(name) <= limit {
		return name
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(name[cut]) {
		cut--
	}
	return name[:cut] + "..."
}
