package lifecycle

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// Stop waits for work, rather than abandoning it: the database is closed on the next line.
func TestStopWaitsForTheWork(t *testing.T) {
	g := New()
	var finished atomic.Bool

	if !g.Go(func(ctx context.Context) {
		<-ctx.Done() // what a scan does: it stops when the process says so
		time.Sleep(20 * time.Millisecond)
		finished.Store(true)
	}) {
		t.Fatal("Go refused work on a fresh group")
	}

	time.Sleep(5 * time.Millisecond) // let the goroutine reach the receive
	g.Stop()

	if !finished.Load() {
		t.Fatal("Stop returned before the work did, so its write could land on a closed database")
	}
}

// The context is cancelled by Stop, which is what bounds work that would otherwise run for its own
// timeout.
func TestStopCancelsTheContext(t *testing.T) {
	g := New()
	done := make(chan struct{})

	g.Go(func(ctx context.Context) {
		<-ctx.Done()
		close(done)
	})

	g.Stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the group context was not cancelled")
	}
}

// A request that arrives during shutdown must not start work the process is about to abandon.
func TestGoRefusesAfterStop(t *testing.T) {
	g := New()
	g.Stop()

	ran := false
	if g.Go(func(context.Context) { ran = true }) {
		t.Error("Go accepted work after Stop")
	}
	time.Sleep(10 * time.Millisecond)
	if ran {
		t.Error("the refused function ran anyway")
	}
}
