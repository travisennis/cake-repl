//go:build windows

package cake

func (r *Run) Stop() error { return nil }

func (r *Run) Continue() error { return nil }
