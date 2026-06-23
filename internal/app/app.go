package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/jkrauska/streamfork/internal/api"
	"github.com/jkrauska/streamfork/internal/config"
	"github.com/jkrauska/streamfork/internal/mediamtx"
	"github.com/jkrauska/streamfork/internal/snapshot"
	"github.com/jkrauska/streamfork/internal/supervisor"
)

type App struct {
	cfgPath string
	logger  *slog.Logger

	mu  sync.RWMutex
	cfg config.Config
	mgr *supervisor.Manager
}

func NewApp(cfgPath string, logger *slog.Logger) *App {
	if logger == nil {
		logger = slog.Default()
	}
	return &App{cfgPath: cfgPath, logger: logger}
}

func (a *App) Run(ctx context.Context) error {
	a.logger.Info("streamfork starting", "config", a.cfgPath)

	cfg, configSource, err := a.loadConfig()
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()

	a.logConfigSummary(cfg, configSource)

	a.logger.Info("creating data directories",
		"data_dir", cfg.Server.DataDir,
		"recordings_dir", cfg.Recording.Path,
	)
	if err := os.MkdirAll(cfg.Server.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	if err := os.MkdirAll(cfg.Recording.Path, 0o755); err != nil {
		return fmt.Errorf("create recordings dir: %w", err)
	}

	recordPath := filepath.Join(cfg.Recording.Path, cfg.Recording.SegmentTemplate)
	a.logger.Info("writing mediamtx config",
		"path", cfg.MediaMTX.ConfigPath,
		"input_path", cfg.Input.Path,
		"srt_ingest", fmt.Sprintf(":%d", config.PortSRT),
		"srt_streamid", fmt.Sprintf("publish:%s", cfg.Input.Path),
		"rtsp", cfg.MediaMTX.RTSPURL+"/"+cfg.Input.Path,
		"recording", cfg.Recording.Enabled,
		"record_path", recordPath,
		"record_format", cfg.Recording.Format,
	)
	if err := mediamtx.WriteConfig(cfg); err != nil {
		return fmt.Errorf("write mediamtx config: %w", err)
	}

	a.logger.Info("starting process supervisor",
		"mediamtx_binary", cfg.MediaMTX.Binary,
		"mediamtx_config", cfg.MediaMTX.ConfigPath,
	)
	a.mgr = supervisor.NewManager(cfg, a.logger)
	mtxClient := mediamtx.NewClient(cfg.MediaMTX.APIURL)
	a.mgr.SetInputReady(func(ctx context.Context) (bool, error) {
		return mtxClient.InputPublishing(ctx, cfg.Input.Path)
	})
	if err := a.mgr.Start(ctx); err != nil {
		return err
	}
	defer func() {
		a.logger.Info("stopping process supervisor")
		a.mgr.Stop(ctx)
	}()

	if err := a.waitForMediaMTX(ctx, mtxClient); err != nil {
		a.logger.Warn("mediamtx control API not ready, output workers not started", "err", err)
	} else {
		a.logger.Info("mediamtx control API ready", "url", cfg.MediaMTX.APIURL)

		if err := a.waitForMediaMTXPath(ctx, mtxClient, cfg.Input.Path); err != nil {
			a.logger.Warn("mediamtx input path not ready, output workers not started", "path", cfg.Input.Path, "err", err)
		} else {
			a.logger.Info("mediamtx input path configured", "path", cfg.Input.Path)
			a.logger.Info("letting mediamtx listeners settle", "delay", mediaMTXSettleDelay)
			if err := sleepContext(ctx, mediaMTXSettleDelay); err != nil {
				return err
			}
			a.mgr.StartEnabledOutputs(ctx)
		}
	}

	snap := snapshot.New(cfg.InputRTSPURL(), func(ctx context.Context) (bool, error) {
		return mtxClient.InputPublishing(ctx, cfg.Input.Path)
	}, a.logger)
	go snap.Run(ctx)

	server := api.NewServer(a, a.mgr, mtxClient, snap)

	httpServer := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := net.Listen("tcp", cfg.Server.Listen)
	if err != nil {
		return fmt.Errorf("control API bind %s: %w (another streamfork running?)", cfg.Server.Listen, err)
	}

	errCh := make(chan error, 1)
	go func() {
		a.logger.Info("streamfork ready",
			"control_api", cfg.Server.Listen,
			"web_ui", "/",
			"status_path", "/api/status",
			"srt_publish", fmt.Sprintf("srt://<host>:%d?streamid=publish:%s", config.PortSRT, cfg.Input.Path),
			"outputs_enabled", countEnabledOutputs(cfg.Outputs),
			"outputs_total", len(cfg.Outputs),
		)
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		a.logger.Info("shutdown requested")
	case sig := <-sigCh:
		a.logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-errCh:
		a.logger.Error("control API stopped", "err", err)
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.logger.Info("shutting down control API")
	return httpServer.Shutdown(shutdownCtx)
}

func (a *App) loadConfig() (config.Config, string, error) {
	if _, err := os.Stat(a.cfgPath); err != nil {
		if os.IsNotExist(err) {
			a.logger.Warn("config file not found, using built-in defaults",
				"path", a.cfgPath,
				"hint", "copy configs/streamfork.example.yml to this path",
			)
			cfg, err := config.Load(a.cfgPath)
			return cfg, "defaults", err
		}
		return config.Config{}, "", fmt.Errorf("stat config: %w", err)
	}

	a.logger.Info("loading config", "path", a.cfgPath)
	cfg, err := config.Load(a.cfgPath)
	if err != nil {
		return config.Config{}, "", err
	}
	return cfg, "file", nil
}

func (a *App) logConfigSummary(cfg config.Config, source string) {
	enabled := 0
	for _, out := range cfg.Outputs {
		if out.Enabled {
			enabled++
		}
	}

	logArgs := []any{
		"source", source,
		"control_listen", cfg.Server.Listen,
		"input_path", cfg.Input.Path,
		"recording_enabled", cfg.Recording.Enabled,
		"outputs_total", len(cfg.Outputs),
		"outputs_enabled", enabled,
	}
	if cfg.Input.SRTLatencyMs > 0 {
		logArgs = append(logArgs, "srt_latency_ms", cfg.Input.SRTLatencyMs)
	}
	a.logger.Info("configuration loaded", logArgs...)

	for _, out := range cfg.Outputs {
		if out.Enabled {
			a.logger.Info("output configured (enabled)",
				"id", out.ID,
				"label", out.Label,
				"url", out.URL,
				"stream_key", config.RedactSecret(out.StreamKey),
				"transcode_h264", out.TranscodeH264,
			)
		} else {
			a.logger.Info("output configured (disabled)",
				"id", out.ID,
				"label", out.Label,
			)
		}
	}

	if len(cfg.Outputs) == 0 {
		a.logger.Info("no RTMP outputs configured yet",
			"hint", "add destinations via POST /api/outputs or edit the config file",
		)
	}
}

const (
	mediaMTXAPIWaitTimeout    = 15 * time.Second
	mediaMTXPathWaitTimeout   = 10 * time.Second
	mediaMTXSettleDelay       = 1 * time.Second
	mediaMTXReadyPollInterval = 500 * time.Millisecond
)

func (a *App) waitForMediaMTX(ctx context.Context, client *mediamtx.Client) error {
	a.logger.Info("waiting for mediamtx control API", "timeout", mediaMTXAPIWaitTimeout)

	deadline := time.Now().Add(mediaMTXAPIWaitTimeout)
	attempt := 0
	for time.Now().Before(deadline) {
		attempt++
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := client.Ready(pingCtx)
		cancel()
		if err == nil {
			if attempt > 1 {
				a.logger.Info("mediamtx control API responded", "attempts", attempt)
			}
			return nil
		}

		if attempt == 1 || attempt%5 == 0 {
			a.logger.Info("mediamtx not ready yet, retrying", "attempt", attempt, "err", err)
		}
		if err := sleepContext(ctx, mediaMTXReadyPollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("timed out waiting for mediamtx control API")
}

func (a *App) waitForMediaMTXPath(ctx context.Context, client *mediamtx.Client, path string) error {
	a.logger.Info("waiting for mediamtx input path", "path", path, "timeout", mediaMTXPathWaitTimeout)

	deadline := time.Now().Add(mediaMTXPathWaitTimeout)
	attempt := 0
	for time.Now().Before(deadline) {
		attempt++
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := client.PathConfigured(pingCtx, path)
		cancel()
		if err == nil {
			if attempt > 1 {
				a.logger.Info("mediamtx input path responded", "path", path, "attempts", attempt)
			}
			return nil
		}

		if attempt == 1 || attempt%5 == 0 {
			a.logger.Info("mediamtx input path not ready yet, retrying", "path", path, "attempt", attempt, "err", err)
		}
		if err := sleepContext(ctx, mediaMTXReadyPollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("timed out waiting for mediamtx input path %q", path)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func countEnabledOutputs(outputs []config.Output) int {
	n := 0
	for _, out := range outputs {
		if out.Enabled {
			n++
		}
	}
	return n
}

func (a *App) Get() config.Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}

func (a *App) Save(cfg config.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := cfg.Save(a.cfgPath); err != nil {
		return err
	}
	if err := mediamtx.WriteConfig(cfg); err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()
	return nil
}

func (a *App) ConfigPath() string {
	return a.cfgPath
}

func ConfigPathFromEnv() string {
	if path := os.Getenv("STREAMFORK_CONFIG"); path != "" {
		return path
	}
	return filepath.Join("/data", "streamfork.yml")
}
