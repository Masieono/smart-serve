// Package lifecycle tracks work that must outlive the request or tick that started it, but must not
// outlive the process.
//
// A scan started from the UI button returns its 202 immediately and reads drives for minutes; a
// scheduled scan runs while the operator restarts the container. Both are detached from the context
// that asked for them, which is right, but "detached from the request" was implemented as "detached
// from everything": nothing cancelled them at shutdown, so the process could close the database under
// a write that was still coming.
//
// A Group is the missing middle. Work started through it runs on a context that lives as long as the
// process, and Stop cancels that context and waits for the work to return. The caller closes the
// database after Stop returns, so a write either lands on an open database or is not attempted.
package lifecycle

import (
	"context"
	"sync"
)

// Group runs background work and waits for it at shutdown.
type Group struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	wg     sync.WaitGroup
	closed bool
}

// New returns a Group with a context that is cancelled by Stop.
func New() *Group {
	ctx, cancel := context.WithCancel(context.Background())
	return &Group{ctx: ctx, cancel: cancel}
}

// Context is the process-lifetime context. Everything started through Go derives from it and is
// therefore cancelled at shutdown.
func (g *Group) Context() context.Context { return g.ctx }

// Go runs fn on the group's context and reports whether it started.
//
// It returns false once Stop has been called: a request that arrives during shutdown should be told
// so rather than starting work the process is about to abandon.
func (g *Group) Go(fn func(context.Context)) bool {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return false
	}
	g.wg.Add(1)
	g.mu.Unlock()

	go func() {
		defer g.wg.Done()
		fn(g.ctx)
	}()
	return true
}

// Stop cancels the group's context and waits for every function started through Go to return. It
// blocks for as long as that work honours its context, which is why the work is bounded by its own
// timeouts.
func (g *Group) Stop() {
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()

	g.cancel()
	g.wg.Wait()
}

// Wait blocks until the work started so far has returned, without cancelling it. For tests and for a
// caller that wants the drain without the stop.
func (g *Group) Wait() { g.wg.Wait() }
