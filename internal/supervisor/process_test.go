package supervisor

import (
	"context"
	"log/slog"
	"runtime"
	"testing"
	"time"
)

func TestManagedProcessStopDoesNotBlockWait(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep command differs on windows")
	}

	proc := newManagedProcess("sleep", []string{"30"}, "sleep", slog.Default())
	if err := proc.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	waitDone := make(chan struct{})
	go func() {
		_ = proc.wait()
		close(waitDone)
	}()

	stopAt := time.Now()
	proc.stop()
	if elapsed := time.Since(stopAt); elapsed > 500*time.Millisecond {
		t.Fatalf("stop blocked for %v, want immediate return", elapsed)
	}

	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("wait did not complete after stop")
	}
}

func TestManagedProcessDoubleWaitSafe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep command differs on windows")
	}

	proc := newManagedProcess("sleep", []string{"30"}, "sleep", slog.Default())
	if err := proc.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	waitDone := make(chan struct{})
	go func() {
		_ = proc.wait()
		close(waitDone)
	}()

	proc.stop()

	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first wait did not complete")
	}

	if err := proc.wait(); err == nil {
		t.Fatal("expected error waiting on stopped process")
	}
}
