package app

import "strings"

func completeSlash(input string, knownSessions []string) (matches []string, ok bool) {
	if !strings.HasPrefix(input, "/") {
		return nil, false
	}

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

func appendUnique(list []string, v string) []string {
	for _, existing := range list {
		if existing == v {
			return list
		}
	}
	return append(list, v)
}
