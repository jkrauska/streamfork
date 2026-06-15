package snapshot

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"sync"
	"time"
)

const (
	defaultInterval = 5 * time.Second
	captureTimeout  = 10 * time.Second
)

type InputReadyFunc func(ctx context.Context) (bool, error)

type Snapshotter struct {
	rtspURL    string
	inputReady InputReadyFunc
	ffmpeg     string
	interval   time.Duration
	logger     *slog.Logger

	mu    sync.RWMutex
	image []byte
	at    time.Time
}

func New(rtspURL string, inputReady InputReadyFunc, logger *slog.Logger) *Snapshotter {
	if logger == nil {
		logger = slog.Default()
	}
	return &Snapshotter{
		rtspURL:    rtspURL,
		inputReady: inputReady,
		ffmpeg:     "ffmpeg",
		interval:   defaultInterval,
		logger:     logger.With("component", "snapshot"),
	}
}

func (s *Snapshotter) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.captureOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.captureOnce(ctx)
		}
	}
}

func (s *Snapshotter) captureOnce(ctx context.Context) {
	if s.inputReady != nil {
		ready, err := s.inputReady(ctx)
		if err != nil || !ready {
			return
		}
	}

	image, err := captureFrame(ctx, s.ffmpeg, s.rtspURL)
	if err != nil {
		s.logger.Debug("snapshot capture failed", "err", err)
		return
	}

	s.mu.Lock()
	s.image = image
	s.at = time.Now().UTC()
	s.mu.Unlock()
}

func (s *Snapshotter) Image() ([]byte, time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.image) == 0 {
		return nil, time.Time{}, false
	}
	out := make([]byte, len(s.image))
	copy(out, s.image)
	return out, s.at, true
}

func captureFrame(ctx context.Context, ffmpegBin, rtspURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, captureTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffmpegBin,
		"-hide_banner",
		"-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-i", rtspURL,
		"-an",
		"-frames:v", "1",
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"pipe:1",
	)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	if stdout.Len() == 0 {
		return nil, errors.New("ffmpeg returned empty snapshot")
	}
	return stdout.Bytes(), nil
}
