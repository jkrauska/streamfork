package metrics

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"sync"
)

type Progress struct {
	Frame       int64
	FPS        float64
	BitrateKbps float64
	Speed      float64
	DropFrames int64
	DupFrames  int64
	OutTimeUS  int64
}

type ProgressReader struct {
	mu   sync.RWMutex
	last Progress
}

func NewProgressReader(r io.Reader) *ProgressReader {
	pr := &ProgressReader{}
	go pr.readLoop(r)
	return pr
}

func (pr *ProgressReader) readLoop(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		pr.apply(key, value)
	}
}

func (pr *ProgressReader) apply(key, value string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	switch key {
	case "frame":
		pr.last.Frame, _ = strconv.ParseInt(value, 10, 64)
	case "fps":
		pr.last.FPS, _ = strconv.ParseFloat(value, 64)
	case "bitrate":
		// ffmpeg reports e.g. "4123.4kbits/s"
		value = strings.TrimSuffix(value, "kbits/s")
		pr.last.BitrateKbps, _ = strconv.ParseFloat(value, 64)
	case "speed":
		value = strings.TrimSuffix(value, "x")
		pr.last.Speed, _ = strconv.ParseFloat(value, 64)
	case "drop_frames":
		pr.last.DropFrames, _ = strconv.ParseInt(value, 10, 64)
	case "dup_frames":
		pr.last.DupFrames, _ = strconv.ParseInt(value, 10, 64)
	case "out_time_us":
		pr.last.OutTimeUS, _ = strconv.ParseInt(value, 10, 64)
	}
}

func (pr *ProgressReader) Snapshot() Progress {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return pr.last
}
