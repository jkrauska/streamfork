package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jkrauska/streamfork/internal/app"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfgPath := app.ConfigPathFromEnv()
	logger.Info("streamfork boot",
		"config", cfgPath,
		"streamfork_config_env", os.Getenv("STREAMFORK_CONFIG"),
	)

	application := app.NewApp(cfgPath, logger)
	if err := application.Run(context.Background()); err != nil {
		logger.Error("streamfork exited", "err", err)
		os.Exit(1)
	}
}
