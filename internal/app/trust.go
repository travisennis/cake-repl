package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/travisennis/cake-repl/internal/cake"
	"github.com/travisennis/cake-repl/internal/ui"
)

const trustProbeTimeout = 10 * time.Second

type trustStatusMsg struct {
	status cake.TrustStatus
	err    error
}

func projectTrustOverridden() bool {
	return os.Getenv("CAKE_TRUST_PROJECT") == "1" ||
		os.Getenv("CAKE_PROJECT_CONFIG") == "off"
}

func (m *Model) startDeferredTrustProbe() tea.Cmd {
	if !m.trustDeferred {
		return nil
	}
	m.trustDeferred = false
	return startTrustProbeCmd(m.cfg)
}

func (m *Model) startDeferredTrustProbeIfIdle() tea.Cmd {
	if m.replay.phase() == replayLoading {
		return nil
	}
	return m.startDeferredTrustProbe()
}

func startTrustProbeCmd(cfg Config) tea.Cmd {
	if cfg.CakeBin == "" || projectTrustOverridden() {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), trustProbeTimeout)
		defer cancel()
		done := make(chan struct{})
		if cfg.Children != nil {
			cfg.Children.Register("trust status", cancel, done)
		}
		defer close(done)
		status, err := cake.ReadTrustStatus(ctx, cake.TrustOptions{
			Bin:      cfg.CakeBin,
			Cwd:      cfg.Cwd,
			DebugLog: cfg.DebugLog,
		})
		return trustStatusMsg{status: status, err: err}
	}
}

func (m *Model) handleTrustStatus(msg trustStatusMsg) {
	if msg.err != nil {
		m.logDebug("trust status probe: " + msg.err.Error())
		return
	}
	if !msg.status.NeedsAttention() {
		return
	}
	m.appendItem(ui.Item{Kind: ui.KindWarning, Text: trustWarning(msg.status)})
}

func trustWarning(status cake.TrustStatus) string {
	root := status.ProjectRoot
	if root == "" {
		root = "this project"
	}
	switch status.State {
	case cake.TrustStateUntrusted:
		return fmt.Sprintf(
			"project configuration is untrusted: %s. Run cake trust in %s before prompting; prompts exit 3 until it is trusted.",
			trustAuthoritySummary(status), root,
		)
	case cake.TrustStateChanged:
		return fmt.Sprintf(
			"project configuration changed since it was trusted: %s. Run cake trust in %s to review and trust the new snapshot; prompts exit 3 until then.",
			trustChangeSummary(status), root,
		)
	case cake.TrustStateInvalid:
		reason := strings.TrimSpace(status.InvalidReason)
		if reason == "" {
			reason = "the trust record cannot be used"
		}
		return fmt.Sprintf(
			"project configuration trust is invalid for %s: %s. Run cake trust in %s to review and replace the record; prompts exit 3 until it is trusted.",
			trustAuthoritySummary(status), reason, root,
		)
	default:
		return fmt.Sprintf(
			"project configuration trust state is %q: %s. Run cake trust --status in %s.",
			string(status.State), trustAuthoritySummary(status), root,
		)
	}
}

func trustAuthoritySummary(status cake.TrustStatus) string {
	paths := make([]string, 0, len(status.Authority))
	keys := make(map[string][]string, len(status.Authority))
	for _, source := range status.Authority {
		if source.Path == "" {
			continue
		}
		if _, seen := keys[source.Path]; !seen {
			paths = append(paths, source.Path)
		}
		keys[source.Path] = append(keys[source.Path], source.Keys...)
	}
	if len(paths) == 0 {
		return "authority-bearing project configuration"
	}
	details := make([]string, 0, min(len(paths), 2))
	for _, path := range paths[:min(len(paths), 2)] {
		detail := path
		if len(keys[path]) > 0 {
			detail += ": " + strings.Join(keys[path], ", ")
		}
		details = append(details, detail)
	}
	suffix := ""
	if len(paths) > len(details) {
		suffix = fmt.Sprintf(", … and %d more", len(paths)-len(details))
	}
	return fmt.Sprintf("%d authority-bearing file%s (%s%s)",
		len(paths), plural(len(paths)), strings.Join(details, ", "), suffix)
}

func trustChangeSummary(status cake.TrustStatus) string {
	if len(status.Changes) == 0 {
		return trustAuthoritySummary(status)
	}
	details := make([]string, 0, min(len(status.Changes), 2))
	for _, change := range status.Changes[:min(len(status.Changes), 2)] {
		detail := change.Path
		if change.Change != "" {
			detail += ": " + change.Change
		}
		details = append(details, detail)
	}
	suffix := ""
	if len(status.Changes) > len(details) {
		suffix = fmt.Sprintf(", … and %d more", len(status.Changes)-len(details))
	}
	return fmt.Sprintf("%d changed file%s (%s%s)",
		len(status.Changes), plural(len(status.Changes)), strings.Join(details, ", "), suffix)
}
