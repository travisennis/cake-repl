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

// trustProbeTimeout bounds the startup trust-status probe. The command reads
// and hashes project .cake files, so it is normally fast; the deadline keeps a
// wedged helper from leaving a startup diagnostic pending indefinitely.
const trustProbeTimeout = 10 * time.Second

// trustStatusMsg delivers the result of the startup project-trust probe.
type trustStatusMsg struct {
	status cake.TrustStatus
	err    error
}

// projectTrustOverridden reports whether cake's one-run trust controls make the
// next prompt independent of the project's persistent trust record. `cake trust
// --status` always inspects the project regardless of these variables, so
// warning from its output would be false in either case. Only cake's documented
// values count.
func projectTrustOverridden() bool {
	return os.Getenv("CAKE_TRUST_PROJECT") == "1" ||
		os.Getenv("CAKE_PROJECT_CONFIG") == "off"
}

// startTrustProbeCmd returns the read-only startup probe of project
// configuration trust, or nil when no engine binary is configured or cake's
// one-run trust controls have already decided the question. The probe runs off
// the Update loop and registers with the process-level child registry while it
// runs, so quit and the unobserved-exit path cancel and reap it (ADR 034).
//
// The probe is advisory: its result appends one warning when cake will refuse
// project authority, and every failure is silent apart from the debug log so an
// older cake binary cannot break startup.
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

// handleTrustStatus applies one probe result. Success with a usable trust state
// is silent; an untrusted, changed, invalid, or unrecognized state appends one
// warning when the probe returns during startup. Probe failures never reach the
// timeline: an older cake, malformed output, or timeout is a debug-log detail,
// not a user error.
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

// trustWarning is the one-line advisory for a project whose configuration cake
// will not apply. Paths and keys come from cake's diagnostic document and are
// sanitized by the render boundary like every other timeline item.
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

// trustAuthoritySummary names how many authority-bearing files cake reported,
// with the first two files and their authority keys. Project-relative paths
// keep the warning independent of where the checkout lives.
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

// trustChangeSummary names how many files differ from the trusted snapshot.
// When cake reports no change rows, it falls back to the authority summary so
// the warning still says what needs review.
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
