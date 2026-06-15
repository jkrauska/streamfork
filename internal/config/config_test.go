package config

import (
	"encoding/json"
	"testing"
)

func TestOutputValidate(t *testing.T) {
	valid := Output{
		ID:        "yt",
		Label:     "YouTube",
		URL:       "rtmp://a.rtmp.youtube.com/live2",
		StreamKey: "secret-key",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid output: %v", err)
	}

	invalid := Output{
		Label:     "Bad",
		URL:       "https://example.com",
		StreamKey: "key",
	}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected invalid scheme error")
	}
}

func TestOutputJSONTags(t *testing.T) {
	out := Output{
		ID:            "youtube",
		Label:         "YouTube",
		URL:           "rtmp://a.rtmp.youtube.com/live2",
		StreamKey:     "****key1",
		Enabled:       true,
		TranscodeH264: false,
	}

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{"id", "label", "url", "stream_key", "enabled", "transcode_h264"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("missing json field %q in %s", key, string(data))
		}
	}
}

func TestDestinationURL(t *testing.T) {
	out := Output{
		URL:       "rtmps://live.example.com/app/",
		StreamKey: "/stream-key",
	}
	got := out.DestinationURL()
	want := "rtmps://live.example.com/app/stream-key"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
