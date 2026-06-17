package app

// promptHistory holds previously submitted inputs for up/down-arrow recall.
// It is a pure state machine so recall transitions stay testable. idx ==
// len(entries) means not navigating; the user's in-progress input is stashed
// in draft when navigation starts and restored when they walk back down past
// the newest entry.
type promptHistory struct {
	entries []string
	idx     int
	draft   string
}

// Add records a submitted input and ends any navigation. Empty inputs and
// consecutive duplicates are not recorded. It returns whether the entry was
// appended (false for empty or consecutive duplicate inputs).
func (h *promptHistory) Add(text string) bool {
	added := text != "" && (len(h.entries) == 0 || h.entries[len(h.entries)-1] != text)
	if added {
		h.entries = append(h.entries, text)
	}
	h.idx = len(h.entries)
	h.draft = ""
	return added
}

// Prev steps to the previous entry, stashing the current input as the draft
// when navigation starts. ok is false at the oldest entry (or with no
// history), in which case the input should be left alone.
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

// Next steps to the following entry, returning the stashed draft when
// stepping past the newest one. ok is false when not navigating.
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
