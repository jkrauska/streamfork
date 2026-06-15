package metrics

import (
	"strings"
	"testing"
)

func TestProgressReader(t *testing.T) {
	input := strings.NewReader("frame=100\nfps=29.97\nbitrate=4000.0kbits/s\nspeed=1.01x\ndrop_frames=2\n")
	pr := &ProgressReader{}
	pr.readLoop(input)

	snap := pr.Snapshot()
	if snap.Frame != 100 {
		t.Fatalf("frame = %d", snap.Frame)
	}
	if snap.FPS != 29.97 {
		t.Fatalf("fps = %f", snap.FPS)
	}
	if snap.BitrateKbps != 4000.0 {
		t.Fatalf("bitrate = %f", snap.BitrateKbps)
	}
	if snap.Speed != 1.01 {
		t.Fatalf("speed = %f", snap.Speed)
	}
	if snap.DropFrames != 2 {
		t.Fatalf("drop_frames = %d", snap.DropFrames)
	}
}
