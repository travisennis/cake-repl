package app

import (
	"strings"

	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

// nativeCompactionState is the REPL's view of Cake's in-run context
// compaction, separate from compaction, which owns the opt-in between-turn
// compactor. The run owns the process, so this state only renders the
// lifecycle and remembers the checkpoint estimates its end may need. Zero
// values mean no operation is pending and no checkpoint is stashed.
type nativeCompactionState struct {
	// pending is set between a live compaction_start and its end. notice
	// identifies the start row by stable identity so removing it never targets
	// a row that took its place.
	pending bool
	notice  noticeID

	// checkpointSeen records that the current lifecycle already accounted for a
	// checkpoint, either by stashing it under a pending start or by appending
	// the bare marker. before, after, and retained are the checkpoint fallback
	// for an end that omits them.
	checkpointSeen bool
	before         int
	after          int
	retained       int
	// window is the context window the start reported, when it did. The end
	// record carries no window, so the committed estimate keeps this one.
	window int
}

// reset clears the lifecycle at a timeline or session boundary. A notice
// handle deliberately does not need removal first: its identity is never
// reused, and the boundary has already cleared the rows.
func (s *nativeCompactionState) reset() {
	*s = nativeCompactionState{}
}

// nativeCompactionStart appends the one start notice for a prepared candidate.
// A start that arrives while an older lifecycle is still pending resolves the
// older row first, so a malformed or truncated stream cannot leave two pending
// starts or suppress the next checkpoint.
func (m *Model) nativeCompactionStart(e cake.CompactionStart) {
	if m.native.pending {
		m.settleNativeCompaction("context compaction interrupted")
	}
	// A start begins a new lifecycle. Drop any estimates stashed by a previous
	// bare checkpoint so this end cannot inherit them when it omits its own.
	m.native = nativeCompactionState{window: e.ContextWindow}
	text := "context compaction started"
	var details []string
	if trigger := strings.TrimSpace(e.Trigger); trigger != "" {
		details = append(details, trigger)
	}
	if e.EstimatedBeforeTokens > 0 {
		details = append(details, ui.CompactTokens(e.EstimatedBeforeTokens)+" before")
	}
	if len(details) > 0 {
		text += " · " + strings.Join(details, ", ")
	}
	m.native.notice = m.appendNotice(ui.Item{Kind: ui.KindInfo, Text: text})
	m.native.pending = true
}

// nativeCompactionCheckpoint handles one checkpoint. Under a pending start it
// contributes no second marker: its estimates are kept for the end, which
// resolves the start into the one committed row. A bare checkpoint, as older
// Cake streams or a lifecycle whose start was cleared produce, keeps the
// existing marker and never seeds the live estimate.
func (m *Model) nativeCompactionCheckpoint(e cake.Compaction) {
	m.native.checkpointSeen = true
	m.native.before = e.EstimatedInput.BeforeTokens
	m.native.after = e.EstimatedInput.AfterTokens
	m.native.retained = e.RetainedCount
	if m.native.pending {
		return
	}
	m.appendItem(ui.Item{
		Kind: ui.KindInfo,
		Text: compactionMarker(e.EstimatedInput.BeforeTokens, e.EstimatedInput.AfterTokens, e.RetainedCount),
	})
}

// nativeCompactionEnd resolves a live lifecycle into exactly one row. A
// committed end replaces the start notice with the shared success marker and
// seeds the status estimate from the end's after figure. A committed end whose
// checkpoint was already rendered adds no second marker. Skip and failure ends
// stay visible even when no start or checkpoint preceded them, because Cake
// emits those attempts as an end-only record.
func (m *Model) nativeCompactionEnd(e cake.CompactionEnd) {
	before := e.EstimatedBeforeTokens
	if before <= 0 {
		before = m.native.before
	}
	after := e.EstimatedAfterTokens
	if after <= 0 {
		after = m.native.after
	}
	retained := e.RetainedCount
	if retained <= 0 {
		retained = m.native.retained
	}

	switch e.Status {
	case cake.CompactionStatusCommitted:
		pending := m.native.pending
		checkpointSeen := m.native.checkpointSeen
		m.removeNativeNotice()
		if pending || !checkpointSeen {
			m.appendItem(ui.Item{Kind: ui.KindInfo, Text: compactionMarker(before, after, retained)})
		}
		m.session.OnCompactionEstimate(after, m.native.window)

	case cake.CompactionStatusNoOp, cake.CompactionStatusNoFit:
		m.removeNativeNotice()
		m.appendItem(ui.Item{
			Kind: ui.KindInfo,
			Text: compactionSkippedText(e.Reason, m.cfg.OutputLimit),
		})

	case cake.CompactionStatusFailed:
		m.removeNativeNotice()
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: nativeCompactionFailedText(e.Reason, m.cfg.OutputLimit)})

	default:
		m.removeNativeNotice()
		status := strings.TrimSpace(e.Status)
		if status == "" {
			status = "unknown"
		}
		m.appendItem(ui.Item{Kind: ui.KindInfo, Text: "context compaction ended: " + status})
	}
	m.native.reset()
}

// nativeCompactionEndReplay renders a replayed end that has no checkpoint. A
// committed end adds nothing because the checkpoint record is already the one
// marker and estimate seed; a skip or failure may exist only as an end record,
// so it stays visible.
func (m *Model) nativeCompactionEndReplay(e cake.CompactionEnd) {
	switch e.Status {
	case cake.CompactionStatusCommitted:
		return
	case cake.CompactionStatusNoOp, cake.CompactionStatusNoFit:
		m.appendItem(ui.Item{
			Kind: ui.KindInfo,
			Text: compactionSkippedText(e.Reason, m.cfg.OutputLimit),
		})
	case cake.CompactionStatusFailed:
		m.appendItem(ui.Item{Kind: ui.KindWarning, Text: nativeCompactionFailedText(e.Reason, m.cfg.OutputLimit)})
	default:
		status := strings.TrimSpace(e.Status)
		if status == "" {
			status = "unknown"
		}
		m.appendItem(ui.Item{Kind: ui.KindInfo, Text: "context compaction ended: " + status})
	}
}

// settleNativeCompaction resolves a start that never received its end when the
// run finishes, so a start row cannot keep reading as pending.
func (m *Model) settleNativeCompaction(text string) {
	m.removeNativeNotice()
	m.appendItem(ui.Item{Kind: ui.KindWarning, Text: text})
	m.native.reset()
}

// removeNativeNotice detaches the pending start row and clears its handle.
func (m *Model) removeNativeNotice() {
	id := m.native.notice
	m.native.notice = 0
	m.native.pending = false
	m.removeNotice(id)
}

// nativeCompactionFailedText names a failed attempt. The reason is audit
// metadata at most 1,024 bytes on the wire; the timeline clip keeps a long
// provider message from dominating the row.
func nativeCompactionFailedText(reason string, outputLimit int) string {
	text := "context compaction failed"
	if reason = strings.TrimSpace(reason); reason != "" {
		text += ": " + ui.TruncateOutput(reason, outputLimit)
	}
	return text
}
