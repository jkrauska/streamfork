package supervisor

import (
	"testing"
	"time"

	"github.com/jkrauska/streamfork/internal/config"
)

func TestOutputStatusBackoffFields(t *testing.T) {
	retryAt := time.Now().Add(2 * time.Second)
	w := &outputWorker{
		cfg: configOutput("gamechanger"),
		state: StateReconnecting,
		retryAt: &retryAt,
		backoffDuration: 2 * time.Second,
		lastError: "exit status 8",
	}

	status := w.status()
	if !status.Backoff {
		t.Fatal("expected backoff=true")
	}
	if status.RetryAt == nil {
		t.Fatal("expected retry_at")
	}
	if status.BackoffSec != 2 {
		t.Fatalf("backoff_sec=%v, want 2", status.BackoffSec)
	}
	if status.RetryInSec <= 0 || status.RetryInSec > 2 {
		t.Fatalf("retry_in_sec=%v, want (0,2]", status.RetryInSec)
	}
}

func TestOutputStatusNoBackoffWhenLive(t *testing.T) {
	w := &outputWorker{
		cfg:   configOutput("gamechanger"),
		state: StateLive,
	}

	status := w.status()
	if status.Backoff {
		t.Fatal("expected backoff=false when live")
	}
	if status.RetryAt != nil {
		t.Fatal("expected retry_at omitted when live")
	}
}

func configOutput(id string) config.Output {
	return config.Output{ID: id, Label: id, Enabled: true}
}