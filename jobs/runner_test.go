package jobs

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunner_RecoversFromPanic pins the property the runner exists for: a
// panic in one tick must not kill the goroutine (or the process) - subsequent
// ticks keep running.
func TestRunner_RecoversFromPanic(t *testing.T) {
	var ticks atomic.Int64

	runner := NewRunner("panicky", 20, 10, func(ctx context.Context) {
		n := ticks.Add(1)
		if n == 1 {
			panic("tick 1 explodes")
		}
	})
	defer runner.Close()

	deadline := time.After(2 * time.Second)
	for ticks.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("only %d ticks ran after the panic; runner goroutine died", ticks.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestRunner_TickContextHasDeadline(t *testing.T) {
	gotDeadline := make(chan bool, 1)

	runner := NewRunner("deadline-check", 20, 500, func(ctx context.Context) {
		select {
		case gotDeadline <- func() bool { _, ok := ctx.Deadline(); return ok }():
		default:
		}
	})
	defer runner.Close()

	select {
	case ok := <-gotDeadline:
		if !ok {
			t.Fatal("tick context has no deadline")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tick never ran")
	}
}

func TestRunner_CloseStopsTicking(t *testing.T) {
	var ticks atomic.Int64
	runner := NewRunner("stoppable", 10, 5, func(ctx context.Context) {
		ticks.Add(1)
	})

	time.Sleep(50 * time.Millisecond)
	if err := runner.Close(); err != nil {
		t.Fatal(err)
	}
	after := ticks.Load()
	time.Sleep(50 * time.Millisecond)
	if ticks.Load() != after {
		t.Fatalf("runner kept ticking after Close: %d -> %d", after, ticks.Load())
	}
}
