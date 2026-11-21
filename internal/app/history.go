package app

type promptHistory struct {
	entries []string
	idx     int
	draft   string
}

func (h *promptHistory) Add(text string) bool {
	added := text != "" && (len(h.entries) == 0 || h.entries[len(h.entries)-1] != text)
	if added {
		h.entries = append(h.entries, text)
	}
	h.idx = len(h.entries)
	h.draft = ""
	return added
}

func (h *promptHistory) Prev(current string) (text string, ok bool) {
	if h.idx == 0 {
		return "", false
	}
	if h.idx == len(h.entries) {
		h.draft = current
	}
	h.idx--
	return h.entries[h.idx], true
}

func (h *promptHistory) Next() (text string, ok bool) {
	if h.idx >= len(h.entries) {
		return "", false
	}
	h.idx++
	if h.idx == len(h.entries) {
		return h.draft, true
	}
	return h.entries[h.idx], true
}
