package config

import "testing"

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
