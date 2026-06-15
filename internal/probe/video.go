package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const defaultTimeout = 12 * time.Second

type VideoStats struct {
	Width  int
	Height int
	FPS    float64
}

type ffprobeOutput struct {
	Streams []struct {
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		RFrameRate   string `json:"r_frame_rate"`
		AvgFrameRate string `json:"avg_frame_rate"`
	} `json:"streams"`
}

func Video(ctx context.Context, ffprobeBin, rtspURL string) (VideoStats, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffprobeBin,
		"-hide_banner",
		"-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-i", rtspURL,
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,r_frame_rate,avg_frame_rate",
		"-of", "json",
	)

	out, err := cmd.Output()
	if err != nil {
		return VideoStats{}, fmt.Errorf("ffprobe: %w", err)
	}

	var parsed ffprobeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return VideoStats{}, fmt.Errorf("parse ffprobe output: %w", err)
	}
	if len(parsed.Streams) == 0 {
		return VideoStats{}, fmt.Errorf("ffprobe: no video stream")
	}

	stream := parsed.Streams[0]
	if stream.Width <= 0 || stream.Height <= 0 {
		return VideoStats{}, fmt.Errorf("ffprobe: missing video dimensions")
	}

	fps := parseFrameRate(stream.RFrameRate)
	if fps <= 0 {
		fps = parseFrameRate(stream.AvgFrameRate)
	}

	return VideoStats{
		Width:  stream.Width,
		Height: stream.Height,
		FPS:    fps,
	}, nil
}

func parseFrameRate(value string) float64 {
	value = strings.TrimSpace(value)
	if value == "" || value == "0/0" {
		return 0
	}

	numStr, denStr, ok := strings.Cut(value, "/")
	if !ok {
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0
		}
		return f
	}

	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0
	}
	den, err := strconv.ParseFloat(denStr, 64)
	if err != nil || den == 0 {
		return 0
	}
	return num / den
}
