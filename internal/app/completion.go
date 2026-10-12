package app

import "strings"

// completeSlash returns all completion replacements for the given input.
// ok is false when input is not a slash command or has no matches.
//
// Each match is a COMPLETE replacement for the input, so a completion always
// leaves the composer holding a parseable command rather than a bare UUID:
//
//   - For a bare "/" it returns all known commands (cycling from first to last).
//   - For "/resume <partial>" it returns "/resume <uuid>" for each known session
//     whose id starts with <partial>; for a bare "/resume " (no partial) it
//     returns every known session. "/resume" without the trailing space stays a
//     command-name match and falls through to commandTable.
//   - For any other slash prefix it matches against commandTable.
//
// Known session ids are deduplicated so a session whose SessionID equals its
// ResumeID is offered once, preserving first-seen order.
func completeSlash(input string, knownSessions []string) (matches []string, ok bool) {
	if !strings.HasPrefix(input, "/") {
		return nil, false
	}

	// /resume with an optional partial UUID.
	if strings.HasPrefix(input, "/resume ") {
		partial := strings.TrimPrefix(input, "/resume ")
		for _, s := range knownSessions {
			if partial != "" && !strings.HasPrefix(s, partial) {
				continue
			}
			matches = appendUnique(matches, "/resume "+s)
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

// appendUnique appends v to list unless it is already present.
func appendUnique(list []string, v string) []string {
	for _, existing := range list {
		if existing == v {
			return list
		}
	}
	return append(list, v)
}
