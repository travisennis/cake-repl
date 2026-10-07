package app

import (
	"testing"
)

// TestHandleTabComplete exercises the completion cycling state machine.
//
// Scenarios:
//  1. Non-slash input is a no-op
//  2. Unknown slash prefix is a no-op
//  3. Single match completes directly, no cycle state
//  4. Multiple matches start a cycle (prefix/matches/idx set)
//  5. Input change during cycle resets and computes fresh
func TestHandleTabComplete(t *testing.T) {
	// --- 1. non-slash input is a no-op ---
	t.Run("non slash noop", func(t *testing.T) {
		m := newLaidOutModel()
		m.input.SetValue("hello")

		tm, _ := m.handleTabComplete()
		got := tm.(Model)

		if got.input.Value() != "hello" {
			t.Errorf("input = %q, want %q", got.input.Value(), "hello")
		}
		if got.completionPrefix != "" {
			t.Errorf("completionPrefix = %q, want empty", got.completionPrefix)
		}
		if got.completionMatches != nil {
			t.Errorf("completionMatches = %v, want nil", got.completionMatches)
		}
		if got.completionIdx != 0 {
			t.Errorf("completionIdx = %d, want 0", got.completionIdx)
		}
	})

	// --- 2. unknown slash prefix is a no-op ---
	t.Run("unknown slash noop", func(t *testing.T) {
		m := newLaidOutModel()
		m.input.SetValue("/z")

		tm, _ := m.handleTabComplete()
		got := tm.(Model)

		if got.input.Value() != "/z" {
			t.Errorf("input = %q, want %q", got.input.Value(), "/z")
		}
		if got.completionPrefix != "" {
			t.Errorf("completionPrefix = %q, want empty", got.completionPrefix)
		}
	})

	// --- 3. single match completes directly, no cycle state ---
	t.Run("single match direct complete", func(t *testing.T) {
		m := newLaidOutModel()
		m.input.SetValue("/h") // only /help matches

		tm, _ := m.handleTabComplete()
		got := tm.(Model)

		if got.input.Value() != "/help" {
			t.Errorf("input = %q, want %q", got.input.Value(), "/help")
		}
		if got.completionPrefix != "" {
			t.Errorf("completionPrefix = %q on single match, want empty", got.completionPrefix)
		}
		if got.completionMatches != nil {
			t.Errorf("completionMatches = %v on single match, want nil", got.completionMatches)
		}
	})

	// --- 4. multi-match starts a cycle ---
	t.Run("multi match starts cycle", func(t *testing.T) {
		m := newLaidOutModel()
		m.input.SetValue("/q") // matches /quit and /q

		tm, _ := m.handleTabComplete()
		got := tm.(Model)

		if got.input.Value() != "/quit" {
			t.Errorf("input = %q after first Tab, want %q", got.input.Value(), "/quit")
		}
		if got.completionPrefix != "/q" {
			t.Errorf("completionPrefix = %q, want %q", got.completionPrefix, "/q")
		}
		if len(got.completionMatches) != 2 {
			t.Fatalf("completionMatches = %v, want 2 entries", got.completionMatches)
		}
		if got.completionMatches[0] != "/quit" || got.completionMatches[1] != "/q" {
			t.Errorf("completionMatches = %v, want [\"/quit\" \"/q\"]", got.completionMatches)
		}
		if got.completionIdx != 0 {
			t.Errorf("completionIdx = %d, want 0", got.completionIdx)
		}
	})

	// --- 5. input change during active cycle resets and does a fresh complete ---
	t.Run("input change during cycle resets", func(t *testing.T) {
		m := newLaidOutModel()
		m.input.SetValue("/q")

		m1, _ := m.handleTabComplete() // starts cycle: input=/quit, prefix=/q

		// Change input while cycle is active.
		mid := m1.(Model)
		mid.input.SetValue("/h") // /h only matches /help

		m2, _ := mid.handleTabComplete()
		got := m2.(Model)

		// Fresh complete: /h is a single match, so direct complete, no cycle.
		if got.input.Value() != "/help" {
			t.Errorf("input = %q after reset+complete, want %q", got.input.Value(), "/help")
		}
		if got.completionPrefix != "" {
			t.Errorf("completionPrefix = %q after single-match reset, want empty", got.completionPrefix)
		}
		if got.completionMatches != nil {
			t.Errorf("completionMatches = %v after single-match reset, want nil", got.completionMatches)
		}
	})

	// --- 6. bare slash starts a multi-match cycle ---
	t.Run("bare slash starts cycle", func(t *testing.T) {
		m := newLaidOutModel()
		m.input.SetValue("/") // matches all 9 commands

		tm, _ := m.handleTabComplete()
		got := tm.(Model)

		if got.input.Value() != "/help" {
			t.Errorf("input = %q after first Tab on bare slash, want %q", got.input.Value(), "/help")
		}
		if got.completionPrefix != "/" {
			t.Errorf("completionPrefix = %q, want %q", got.completionPrefix, "/")
		}
		if len(got.completionMatches) != 9 {
			t.Errorf("completionMatches has %d entries, want 9", len(got.completionMatches))
		}
		if got.completionIdx != 0 {
			t.Errorf("completionIdx = %d, want 0", got.completionIdx)
		}
		// First match should be /help
		if got.completionMatches[0] != "/help" {
			t.Errorf("first match = %q, want %q", got.completionMatches[0], "/help")
		}
	})
}

// TestHandleTabCompleteAdvance verifies that pressing Tab a second time advances
// the cycle to the next match.
func TestHandleTabCompleteAdvance(t *testing.T) {
	// Start a cycle with /q → /quit
	m := newLaidOutModel()
	m.input.SetValue("/q")

	m1, _ := m.handleTabComplete() // cycle starts: input=/quit, prefix=/q, matches=[/quit,/q], idx=0

	// Second Tab: advance to /q (idx 1)
	m2, _ := m1.(Model).handleTabComplete()
	got := m2.(Model)

	if got.input.Value() != "/q" {
		t.Errorf("input = %q after second Tab, want %q", got.input.Value(), "/q")
	}
	if got.completionPrefix != "/q" {
		t.Errorf("completionPrefix = %q after second Tab, want %q", got.completionPrefix, "/q")
	}
	if len(got.completionMatches) != 2 {
		t.Fatalf("completionMatches = %v, want 2 entries", got.completionMatches)
	}
	if got.completionIdx != 1 {
		t.Errorf("completionIdx = %d after second Tab, want 1", got.completionIdx)
	}
}

// TestHandleTabCompleteWrap verifies that pressing Tab beyond the end of the
// match list wraps around, restoring the original prefix and ending the cycle.
func TestHandleTabCompleteWrap(t *testing.T) {
	// Start a cycle with /q → /quit
	m := newLaidOutModel()
	m.input.SetValue("/q")

	m1, _ := m.handleTabComplete() // cycle starts: input=/quit, idx=0

	// Second Tab: advance to /q (idx 1)
	m2, _ := m1.(Model).handleTabComplete()

	// Third Tab: wrap — restore prefix /q, clear cycle state
	m3, _ := m2.(Model).handleTabComplete()
	got := m3.(Model)

	if got.input.Value() != "/q" {
		t.Errorf("input = %q after wrap, want %q", got.input.Value(), "/q")
	}
	if got.completionPrefix != "" {
		t.Errorf("completionPrefix = %q after wrap, want empty", got.completionPrefix)
	}
	if got.completionMatches != nil {
		t.Errorf("completionMatches = %v after wrap, want nil", got.completionMatches)
	}
	if got.completionIdx != 0 {
		t.Errorf("completionIdx = %d after wrap, want 0", got.completionIdx)
	}
}

// TestHandleTabCompleteBareSlashCycle verifies cycling through all commands
// starting from bare "/".
func TestHandleTabCompleteBareSlashCycle(t *testing.T) {
	m := newLaidOutModel()
	m.input.SetValue("/")

	// First Tab: start cycle, first match is /help
	m1, _ := m.handleTabComplete()
	got1 := m1.(Model)
	if got1.input.Value() != "/help" {
		t.Errorf("input after first Tab = %q, want %q", got1.input.Value(), "/help")
	}

	// Advance through remaining 8 matches (indices 1..8) then wrap back to "/".
	cur := got1
	for i := 0; i < 9; i++ {
		next, _ := cur.handleTabComplete()
		cur = next.(Model)
	}

	// After 9 more Tabs (8 advances + 1 wrap), we should be back at "/" with
	// no active cycle.
	if cur.input.Value() != "/" {
		t.Errorf("input after full cycle + wrap = %q, want %q", cur.input.Value(), "/")
	}
	if cur.completionPrefix != "" {
		t.Errorf("completionPrefix after full cycle + wrap = %q, want empty", cur.completionPrefix)
	}
	if cur.completionMatches != nil {
		t.Errorf("completionMatches after full cycle + wrap = %v, want nil", cur.completionMatches)
	}
}

// TestHandleTabCompleteResumeCycle verifies cycling through matching known
// session IDs and restoring the original partial UUID on wrap.
func TestHandleTabCompleteResumeCycle(t *testing.T) {
	const (
		first  = "11111111-2222-3333-4444-555555555555"
		second = "11111111-aaaa-bbbb-cccc-dddddddddddd"
		prefix = "/resume 1111"
	)

	m := newLaidOutModel()
	m.session.SessionID = first
	m.session.ResumeID = second
	m.input.SetValue(prefix)

	m1, _ := m.handleTabComplete()
	got := m1.(Model)
	if got.input.Value() != first {
		t.Errorf("input after first Tab = %q, want %q", got.input.Value(), first)
	}

	m2, _ := got.handleTabComplete()
	got = m2.(Model)
	if got.input.Value() != second {
		t.Errorf("input after second Tab = %q, want %q", got.input.Value(), second)
	}

	m3, _ := got.handleTabComplete()
	got = m3.(Model)
	if got.input.Value() != prefix {
		t.Errorf("input after wrap = %q, want %q", got.input.Value(), prefix)
	}
	if got.completionPrefix != "" {
		t.Errorf("completionPrefix after wrap = %q, want empty", got.completionPrefix)
	}
	if got.completionMatches != nil {
		t.Errorf("completionMatches after wrap = %v, want nil", got.completionMatches)
	}
}
