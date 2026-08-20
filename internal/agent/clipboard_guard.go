package agent

import (
	"sync"

	"clipsyncpro/internal/win32"
)

// clipboardWriteGuard serializes agent-originated writes with polling and
// consumes only the exact Windows clipboard sequence produced by that write.
// An external copy of identical content receives a new sequence and is kept.
type clipboardWriteGuard struct {
	mu             sync.Mutex
	sequence       func() uint32
	suppressed     uint32
	hasSuppression bool
}

func newClipboardWriteGuard(sequence func() uint32) *clipboardWriteGuard {
	return &clipboardWriteGuard{sequence: sequence}
}

func (g *clipboardWriteGuard) write(write func() error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := write(); err != nil {
		return err
	}
	g.suppressed = g.sequence()
	g.hasSuppression = true
	return nil
}

func (g *clipboardWriteGuard) read(read func() (win32.ClipboardSnapshot, error)) (win32.ClipboardSnapshot, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	snapshot, err := read()
	if err != nil {
		return snapshot, false, err
	}
	return snapshot, g.consumeSuppression(snapshot.Sequence), nil
}

func (g *clipboardWriteGuard) shouldSuppress(sequence uint32) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.consumeSuppression(sequence)
}

func (g *clipboardWriteGuard) consumeSuppression(sequence uint32) bool {
	if !g.hasSuppression || sequence != g.suppressed {
		return false
	}
	g.hasSuppression = false
	return true
}
