package supervisor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/jkrauska/streamfork/internal/config"
	"github.com/jkrauska/streamfork/internal/metrics"
)

type OutputState string

const (
	StateIdle         OutputState = "idle"
	StateConnecting   OutputState = "connecting"
	StateLive         OutputState = "live"
	StateReconnecting OutputState = "reconnecting"
	StateFailed       OutputState = "failed"
	StateStopped      OutputState = "stopped"
)

type OutputStatus struct {
	ID            string           `json:"id"`
	Label         string           `json:"label"`
	Enabled       bool             `json:"enabled"`
	State         OutputState      `json:"state"`
	RestartCount  int              `json:"restart_count"`
	LastError     string           `json:"last_error,omitempty"`
	Backoff       bool             `json:"backoff,omitempty"`
	BackoffSec    float64          `json:"backoff_sec,omitempty"`
	RetryAt       *time.Time       `json:"retry_at,omitempty"`
	RetryInSec    float64          `json:"retry_in_sec,omitempty"`
	Progress      metrics.Progress `json:"progress"`
	StartedAt     *time.Time       `json:"started_at,omitempty"`
	LastExitAt    *time.Time       `json:"last_exit_at,omitempty"`
}

type InputReadyFunc func(ctx context.Context) (bool, error)

type Manager struct {
	cfg        config.Config
	logger     *slog.Logger
	inputReady InputReadyFunc

	mu      sync.RWMutex
	workers map[string]*outputWorker
	media   *managedProcess
}

func NewManager(cfg config.Config, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		cfg:     cfg,
		logger:  logger,
		workers: make(map[string]*outputWorker),
	}
}

func (m *Manager) SetInputReady(fn InputReadyFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inputReady = fn
}

func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.media != nil {
		return fmt.Errorf("manager already started")
	}

	m.logger.Info("starting mediamtx",
		"binary", m.cfg.MediaMTX.Binary,
		"config", m.cfg.MediaMTX.ConfigPath,
		"srt_listen", fmt.Sprintf(":%d", config.PortSRT),
		"rtsp_listen", fmt.Sprintf(":%d", config.PortRTSP),
		"rtmp_listen", fmt.Sprintf(":%d", config.PortRTMP),
	)

	m.media = newManagedProcess("mediamtx", []string{m.cfg.MediaMTX.ConfigPath}, m.cfg.MediaMTX.Binary, m.logger)
	if err := m.media.start(ctx); err != nil {
		m.media = nil
		return fmt.Errorf("start mediamtx: %w", err)
	}

	return nil
}

func (m *Manager) StartEnabledOutputs(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	enabled := 0
	for _, out := range m.cfg.Outputs {
		if out.Enabled {
			enabled++
		}
	}
	m.logger.Info("starting output workers",
		"outputs_enabled", enabled,
		"outputs_total", len(m.cfg.Outputs),
		"input_rtsp", m.cfg.InputRTSPURL(),
	)

	for _, out := range m.cfg.Outputs {
		if out.Enabled {
			m.ensureWorkerLocked(out)
			continue
		}
		m.logger.Info("skipping disabled output", "id", out.ID, "label", out.Label)
	}
}

func (m *Manager) Stop(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, worker := range m.workers {
		worker.stop()
		delete(m.workers, id)
	}
	if m.media != nil {
		m.media.stop()
		m.media = nil
	}
}

func (m *Manager) ReloadOutputs(outputs []config.Output) {
	var toStop []*outputWorker

	m.mu.Lock()
	m.cfg.Outputs = outputs
	active := make(map[string]struct{}, len(outputs))
	for _, out := range outputs {
		active[out.ID] = struct{}{}
		if out.Enabled {
			m.ensureWorkerLocked(out)
			continue
		}
		if worker, ok := m.workers[out.ID]; ok {
			delete(m.workers, out.ID)
			toStop = append(toStop, worker)
		}
	}

	for id, worker := range m.workers {
		if _, ok := active[id]; !ok {
			delete(m.workers, id)
			toStop = append(toStop, worker)
		}
	}
	m.mu.Unlock()

	for _, worker := range toStop {
		worker.stop()
	}
}

func (m *Manager) StartOutput(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	out, ok := m.findOutputLocked(id)
	if !ok {
		return fmt.Errorf("output %q not found", id)
	}
	m.ensureWorkerLocked(out)
	return nil
}

func (m *Manager) StopOutput(id string) error {
	m.mu.Lock()
	worker, ok := m.workers[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("output %q is not running", id)
	}
	delete(m.workers, id)
	m.mu.Unlock()

	worker.stop()
	return nil
}

func (m *Manager) RestartOutput(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	out, ok := m.findOutputLocked(id)
	if !ok {
		return fmt.Errorf("output %q not found", id)
	}
	if worker, ok := m.workers[id]; ok {
		worker.restart()
		return nil
	}
	m.ensureWorkerLocked(out)
	return nil
}

func (m *Manager) OutputStatuses() []OutputStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]OutputStatus, 0, len(m.cfg.Outputs))
	for _, out := range m.cfg.Outputs {
		status := OutputStatus{
			ID:      out.ID,
			Label:   out.Label,
			Enabled: out.Enabled,
			State:   StateIdle,
		}
		if worker, ok := m.workers[out.ID]; ok {
			status = worker.status()
		} else if !out.Enabled {
			status.State = StateStopped
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func (m *Manager) MediaRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.media != nil && m.media.running()
}

func (m *Manager) findOutputLocked(id string) (config.Output, bool) {
	for _, out := range m.cfg.Outputs {
		if out.ID == id {
			return out, true
		}
	}
	return config.Output{}, false
}

func (m *Manager) ensureWorkerLocked(out config.Output) {
	if worker, ok := m.workers[out.ID]; ok {
		worker.updateConfig(out, m.cfg.InputRTSPURL())
		return
	}
	m.logger.Info("starting output worker",
		"id", out.ID,
		"label", out.Label,
		"input", m.cfg.InputRTSPURL(),
		"destination", out.URL,
		"stream_key", config.RedactSecret(out.StreamKey),
		"mode", outputMode(out),
	)
	worker := newOutputWorker(out, m.cfg.InputRTSPURL(), m.inputReady, m.logger)
	m.workers[out.ID] = worker
	worker.start(context.Background())
}

func outputMode(out config.Output) string {
	if out.TranscodeH264 {
		return "h264-transcode"
	}
	return "hevc-copy"
}

func logProcessOutput(logger *slog.Logger, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		logger.Warn("ffmpeg", "msg", line)
	}
}

type outputWorker struct {
	mu sync.RWMutex

	cfg        config.Output
	inputURL   string
	inputReady InputReadyFunc
	logger     *slog.Logger

	proc          *managedProcess
	progress      *metrics.ProgressReader
	state           OutputState
	restartCount    int
	lastError       string
	startedAt       *time.Time
	lastExitAt      *time.Time
	retryAt         *time.Time
	backoffDuration time.Duration
	cancelWatch     context.CancelFunc
}

func newOutputWorker(out config.Output, inputURL string, inputReady InputReadyFunc, logger *slog.Logger) *outputWorker {
	return &outputWorker{
		cfg:        out,
		inputURL:   inputURL,
		inputReady: inputReady,
		logger:     logger.With("output", out.ID),
		state:      StateConnecting,
	}
}

func (w *outputWorker) updateConfig(out config.Output, inputURL string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cfg = out
	w.inputURL = inputURL
}

func (w *outputWorker) start(ctx context.Context) {
	w.mu.Lock()
	if w.cancelWatch != nil {
		w.mu.Unlock()
		return
	}
	w.state = StateConnecting
	watchCtx, cancel := context.WithCancel(ctx)
	w.cancelWatch = cancel
	w.mu.Unlock()

	go w.runLoop(watchCtx)
}

func (w *outputWorker) runLoop(ctx context.Context) {
	backoff := time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := w.waitForInput(ctx); err != nil {
			return
		}

		if err := w.runOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			w.setError(err)
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		nextBackoff := backoff
		if nextBackoff < 30*time.Second {
			nextBackoff *= 2
		}

		w.beginBackoff(backoff)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff = nextBackoff
	}
}

func (w *outputWorker) waitForInput(ctx context.Context) error {
	w.mu.RLock()
	ready := w.inputReady
	w.mu.RUnlock()
	if ready == nil {
		return nil
	}

	loggedWaiting := false
	for {
		publishing, err := ready(ctx)
		if err == nil && publishing {
			if loggedWaiting {
				w.logger.Info("input stream available")
			}
			return nil
		}

		if !loggedWaiting {
			w.logger.Info("waiting for input stream before connecting ffmpeg")
			loggedWaiting = true
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (w *outputWorker) beginBackoff(current time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()

	retryAt := time.Now().Add(current)
	w.state = StateReconnecting
	w.restartCount++
	w.retryAt = &retryAt
	w.backoffDuration = current
}

func (w *outputWorker) clearBackoffLocked() {
	w.retryAt = nil
	w.backoffDuration = 0
}

func (w *outputWorker) runOnce(ctx context.Context) error {
	w.mu.RLock()
	cfg := w.cfg
	inputURL := w.inputURL
	w.mu.RUnlock()

	args := ffmpegArgs(cfg, inputURL)
	proc := newManagedProcess("ffmpeg", args, "ffmpeg", w.logger)

	cmd, err := proc.initCommand(ctx)
	if err != nil {
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	progress := metrics.NewProgressReader(stdout)
	go logProcessOutput(w.logger, stderr)

	w.mu.Lock()
	w.proc = proc
	w.progress = progress
	now := time.Now()
	w.startedAt = &now
	w.state = StateConnecting
	w.clearBackoffLocked()
	w.mu.Unlock()

	if err := proc.start(ctx); err != nil {
		return err
	}

	w.mu.Lock()
	w.state = StateLive
	w.mu.Unlock()

	w.logger.Info("ffmpeg process running")

	err = proc.wait()
	exitAt := time.Now()
	w.mu.Lock()
	w.lastExitAt = &exitAt
	w.proc = nil
	w.progress = nil
	w.mu.Unlock()

	if err != nil {
		w.mu.Lock()
		w.state = StateFailed
		w.clearBackoffLocked()
		w.mu.Unlock()
		w.logger.Warn("ffmpeg exited", "err", err)
	}
	return err
}

func (w *outputWorker) setError(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastError = err.Error()
}

func (w *outputWorker) stop() {
	w.mu.Lock()
	cancel := w.cancelWatch
	proc := w.proc
	w.cancelWatch = nil
	w.proc = nil
	w.progress = nil
	w.state = StateStopped
	w.clearBackoffLocked()
	w.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if proc != nil {
		proc.stop()
	}
}

func (w *outputWorker) restart() {
	w.stop()
	w.start(context.Background())
}

func (w *outputWorker) status() OutputStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()

	status := OutputStatus{
		ID:           w.cfg.ID,
		Label:        w.cfg.Label,
		Enabled:      w.cfg.Enabled,
		State:        w.state,
		RestartCount: w.restartCount,
		LastError:    w.lastError,
		StartedAt:    w.startedAt,
		LastExitAt:   w.lastExitAt,
	}
	if w.progress != nil {
		status.Progress = w.progress.Snapshot()
	}
	if w.retryAt != nil {
		remaining := time.Until(*w.retryAt).Seconds()
		if remaining > 0 {
			status.Backoff = true
			status.RetryAt = w.retryAt
			status.RetryInSec = remaining
			status.BackoffSec = w.backoffDuration.Seconds()
		}
	}
	return status
}

func ffmpegArgs(out config.Output, inputURL string) []string {
	args := []string{
		"-hide_banner",
		"-nostats",
		"-progress", "pipe:1",
		"-loglevel", "warning",
		"-rtsp_transport", "tcp",
		"-i", inputURL,
	}

	if out.TranscodeH264 {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-c:a", "aac", "-f", "flv")
	} else {
		args = append(args, "-c", "copy", "-f", "flv", "-rtmp_enhanced_codecs", "hvc1,mp4a")
	}

	args = append(args, out.DestinationURL())
	return args
}

type managedProcess struct {
	name   string
	args   []string
	binary string
	logger *slog.Logger

	mu      sync.Mutex
	cmd     *exec.Cmd
	done    chan struct{}
	waitErr error
}

func newManagedProcess(name string, args []string, binary string, logger *slog.Logger) *managedProcess {
	return &managedProcess{
		name:   name,
		args:   args,
		binary: binary,
		logger: logger.With("process", name),
	}
}

func (p *managedProcess) initCommand(ctx context.Context) (*exec.Cmd, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil {
		return nil, fmt.Errorf("%s already running", p.name)
	}

	cmd := exec.CommandContext(ctx, p.binary, p.args...)
	cmd.Stdin = nil
	cmd.Env = os.Environ()
	p.cmd = cmd
	return cmd, nil
}

func (p *managedProcess) start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd == nil {
		cmd := exec.CommandContext(ctx, p.binary, p.args...)
		cmd.Stdin = nil
		cmd.Env = os.Environ()
		p.cmd = cmd
	}

	p.logger.Info("starting process", "binary", p.binary, "args", p.args)
	if err := p.cmd.Start(); err != nil {
		p.cmd = nil
		return err
	}
	p.logger.Info("process started", "pid", p.cmd.Process.Pid)
	p.spawnWaiter()
	return nil
}

func (p *managedProcess) spawnWaiter() {
	p.done = make(chan struct{})
	go func() {
		p.mu.Lock()
		cmd := p.cmd
		p.mu.Unlock()

		var err error
		if cmd != nil {
			err = cmd.Wait()
		}

		p.mu.Lock()
		p.waitErr = err
		p.cmd = nil
		p.mu.Unlock()

		if err != nil {
			p.logger.Warn("process exited", "err", err)
		}
		close(p.done)
	}()
}

func (p *managedProcess) wait() error {
	p.mu.Lock()
	done := p.done
	p.mu.Unlock()
	if done == nil {
		return fmt.Errorf("%s not running", p.name)
	}
	<-done
	p.mu.Lock()
	err := p.waitErr
	p.mu.Unlock()
	return err
}

func (p *managedProcess) stop() {
	p.mu.Lock()
	cmd := p.cmd
	p.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}

func (p *managedProcess) running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cmd != nil
}
