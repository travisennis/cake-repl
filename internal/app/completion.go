package app

import "strings"

// knownCommands is the ordered list of completable slash command names.
// Canonical names come first; aliases follow so the first Tab press shows the
// preferred form. Keep this in sync with ParseCommand.
var knownCommands = []string{
	"/help",
	"/exit",
	"/new",
	"/continue",
	"/resume",
	"/session",
	"/clear",
	"/quit",
	"/q",
}

// completeSlash returns all matching completions for the given input.
// ok is false when input is not a slash command or has no matches.
//
// For a bare "/" it returns all known commands (cycling from first to last).
// For "/resume <partial>" it matches against known session IDs.
// For any other slash prefix it matches against knownCommands.
func completeSlash(input string, knownSessions []string) (matches []string, ok bool) {
	if !strings.HasPrefix(input, "/") {
		return nil, false
	}

	// /resume with an optional partial UUID.
	if strings.HasPrefix(input, "/resume ") {
		partial := strings.TrimPrefix(input, "/resume ")
		if partial == "" {
			if len(knownSessions) > 0 {
				return knownSessions, true
			}
			return nil, false
		}
		for _, s := range knownSessions {
			if strings.HasPrefix(s, partial) {
				matches = append(matches, s)
			}
		}
		if len(matches) > 0 {
			return matches, true
		}
		return nil, false
	}

	for _, cmd := range knownCommands {
		if strings.HasPrefix(cmd, input) {
			matches = append(matches, cmd)
		}
	}

	if len(matches) > 0 {
		return matches, true
	}
	return nil, false
}
