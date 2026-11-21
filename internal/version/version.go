package version

import (
	"runtime/debug"
	"strings"
)

var Binary = "dev"

func Text() string {
	info, _ := debug.ReadBuildInfo()
	return text(info)
}

func text(info *debug.BuildInfo) string {
	lines := []string{"cake-repl " + Binary}
	if info == nil {
		return lines[0]
	}

	var revision, commitTime string
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.time":
			commitTime = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return lines[0]
	}

	revisionLine := "revision " + revision
	if modified {
		revisionLine += " (modified)"
	}
	lines = append(lines, revisionLine)
	if commitTime != "" {
		lines = append(lines, "time "+commitTime)
	}
	return strings.Join(lines, "\n")
}
