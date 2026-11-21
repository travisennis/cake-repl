package app

import (
	"strings"

	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

type nativeCompactionState struct {
	pending bool
	notice  noticeID

	checkpointSeen bool
	before         int
	after          int
	retained       int
	window         int
}

func (s *nativeCompactionState) reset() {
	*s = nativeCompactionState{}
}

func (m *Model) nativeCompactionStart(e cake.CompactionStart) {
	if m.native.pending {
		m.settleNativeCompaction("context compaction interrupted")
	}
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

func (m *Model) settleNativeCompaction(text string) {
	m.removeNativeNotice()
	m.appendItem(ui.Item{Kind: ui.KindWarning, Text: text})
	m.native.reset()
}

func (m *Model) removeNativeNotice() {
	id := m.native.notice
	m.native.notice = 0
	m.native.pending = false
	m.removeNotice(id)
}

func nativeCompactionFailedText(reason string, outputLimit int) string {
	text := "context compaction failed"
	if reason = strings.TrimSpace(reason); reason != "" {
		text += ": " + ui.TrimForDisplay(reason, outputLimit)
	}
	return text
}
