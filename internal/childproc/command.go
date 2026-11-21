package childproc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

const (
	DefaultWaitDelay = 3 * time.Second
	DefaultGrace     = 3 * time.Second
	DefaultStderr    = 8 * 1024
)

type CommandOptions struct {
	WaitDelay          time.Duration
	Grace              time.Duration
	ImmediateGroupKill bool
	OnCancel           func() bool
	DebugLog           io.Writer
}

type Command struct {
	*exec.Cmd

	mu         sync.Mutex
	graceTimer *time.Timer
}

func NewCommand(ctx context.Context, bin string, args []string, cwd string, opts CommandOptions) *Command {
	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- bin intentionally comes from config.
	if cwd != "" {
		cmd.Dir = cwd
	}
	Isolate(cmd)

	waitDelay := opts.WaitDelay
	if waitDelay <= 0 {
		waitDelay = DefaultWaitDelay
	}
	cmd.WaitDelay = waitDelay

	grace := opts.Grace
	if grace <= 0 {
		grace = DefaultGrace
	}
	c := &Command{Cmd: cmd}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return errors.New("process has not started")
		}
		if opts.ImmediateGroupKill {
			return ignoreGone(killGroup(cmd.Process.Pid))
		}
		if err := signalTerm(cmd.Process); err != nil {
			return err
		}
		if opts.OnCancel != nil && !opts.OnCancel() {
			return nil
		}
		c.armGrace(cmd.Process.Pid, grace, opts.DebugLog)
		return nil
	}
	return c
}

func (c *Command) armGrace(pid int, grace time.Duration, logf io.Writer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.graceTimer != nil {
		return
	}
	c.graceTimer = time.AfterFunc(grace, func() {
		if err := killGroup(pid); err != nil && !ProcessGone(err) && logf != nil {
			fmt.Fprintf(logf, "cancel: killing process group %d failed: %v\n", pid, err)
		}
	})
}

func (c *Command) Reaped() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.graceTimer != nil {
		c.graceTimer.Stop()
		c.graceTimer = nil
	}
}

func ProcessGone(err error) bool {
	return processGone(err)
}

func ignoreGone(err error) error {
	if ProcessGone(err) {
		return nil
	}
	return err
}
