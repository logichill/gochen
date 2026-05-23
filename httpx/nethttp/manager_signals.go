package nethttp

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"gochen/errors"
)

// RunWithSignals 执行带Signals。
func (m *Manager) RunWithSignals(ctx context.Context, signals ...os.Signal) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if len(signals) == 0 {
		signals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}
	}
	runCtx, cancel := signal.NotifyContext(ctx, signals...)
	defer cancel()
	return m.Run(runCtx)
}
