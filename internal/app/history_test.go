package app

import "testing"

func TestPromptHistoryEmpty(t *testing.T) {
	h := promptHistory{}
	if _, ok := h.Prev("draft"); ok {
		t.Error("Prev on empty history should not recall")
	}
	if _, ok := h.Next(); ok {
		t.Error("Next on empty history should not recall")
	}
}

func TestPromptHistoryAddFiltering(t *testing.T) {
	h := promptHistory{}
	h.Add("one")
	h.Add("two")
	h.Add("two") // consecutive duplicate dropped
	h.Add("")    // empty dropped
	if len(h.entries) != 2 {
		t.Fatalf("entries = %v, want [one two]", h.entries)
	}
	h.Add("one") // non-consecutive repeat is kept
	if len(h.entries) != 3 {
		t.Errorf("entries = %v, want [one two one]", h.entries)
	}
}

func TestPromptHistoryWalkAndDraft(t *testing.T) {
	h := promptHistory{}
	h.Add("one")
	h.Add("two")

	steps := []struct {
		op   string
		want string
	}{
		{"prev", "two"},
		{"prev", "one"},
		{"next", "two"},
		{"next", "draft"}, // past the newest entry restores the draft
	}
	for i, s := range steps {
		var text string
		var ok bool
		if s.op == "prev" {
			text, ok = h.Prev("draft")
		} else {
			text, ok = h.Next()
		}
		if !ok || text != s.want {
			t.Fatalf("step %d (%s): got %q ok=%v, want %q", i, s.op, text, ok, s.want)
		}
	}
	if _, ok := h.Next(); ok {
		t.Error("Next past the draft should not recall")
	}

	// Walking below the oldest entry stops there.
	h.Prev("draft")
	h.Prev("draft")
	if _, ok := h.Prev("draft"); ok {
		t.Error("Prev below the oldest entry should not recall")
	}
}

func TestPromptHistoryAddEndsNavigation(t *testing.T) {
	h := promptHistory{}
	h.Add("one")
	h.Prev("draft")
	h.Add("two")
	if _, ok := h.Next(); ok {
		t.Error("Add should end navigation")
	}
	if text, ok := h.Prev("other"); !ok || text != "two" {
		t.Errorf("Prev after Add = %q ok=%v, want the newest entry", text, ok)
	}
}
