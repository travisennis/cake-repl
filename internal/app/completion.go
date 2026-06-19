package app

import "strings"

// completeSlash returns all matching completions for the given input.
// ok is false when input is not a slash command or has no matches.
//
// For a bare "/" it returns all known commands (cycling from first to last).
// For "/resume <partial>" it matches against known session IDs.
// For any other slash prefix it matches against commandTable.
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

	for _, entry := range commandTable {
		if strings.HasPrefix(entry.name, input) {
			matches = append(matches, entry.name)
		}
	}

	if len(matches) > 0 {
		return matches, true
	}
	return nil, false
}
