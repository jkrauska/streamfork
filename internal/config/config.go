package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const Version = 1

type Config struct {
	Version   int            `yaml:"version"`
	Server    ServerConfig   `yaml:"server"`
	Input     InputConfig    `yaml:"input"`
	MediaMTX  MediaMTXConfig `yaml:"mediamtx"`
	Recording RecordingConfig `yaml:"recording"`
	Outputs   []Output       `yaml:"outputs"`
}

type ServerConfig struct {
	Listen  string `yaml:"listen"`
	DataDir string `yaml:"data_dir"`
}

type InputConfig struct {
	Path                 string `yaml:"path"`
	SRTPublishPassphrase string `yaml:"srt_publish_passphrase,omitempty"`
	// SRTLatencyMs is the recommended caller latency (Magewell Mini buffer).
	// SRT latency is negotiated by the publisher; MediaMTX cannot override it.
	SRTLatencyMs int `yaml:"srt_latency_ms,omitempty" json:"srt_latency_ms,omitempty"`
}

type MediaMTXConfig struct {
	Binary     string `yaml:"binary"`
	ConfigPath string `yaml:"config_path"`
	APIURL     string `yaml:"api_url"`
	RTSPURL    string `yaml:"rtsp_url"`
}

type RecordingConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Path            string `yaml:"path"`
	SegmentTemplate string `yaml:"segment_template"`
	Format          string `yaml:"format"`
	SegmentDuration string `yaml:"segment_duration"`
	DeleteAfter     string `yaml:"delete_after"`
}

type Output struct {
	ID            string `yaml:"id" json:"id"`
	Label         string `yaml:"label" json:"label"`
	URL           string `yaml:"url" json:"url"`
	StreamKey     string `yaml:"stream_key" json:"stream_key"`
	Enabled       bool   `yaml:"enabled" json:"enabled"`
	TranscodeH264 bool   `yaml:"transcode_h264" json:"transcode_h264"`
}

func Default() Config {
	return Config{
		Version: Version,
		Server: ServerConfig{
			Listen:  fmt.Sprintf("0.0.0.0:%d", PortControlAPI),
			DataDir: "/data",
		},
		Input: InputConfig{
			Path: "field",
		},
		MediaMTX: MediaMTXConfig{
			Binary:     "/usr/local/bin/mediamtx",
			ConfigPath: "/data/mediamtx.yml",
			APIURL:     DefaultMediaMTXAPIURL(),
			RTSPURL:    DefaultRTSPURL(),
		},
		Recording: RecordingConfig{
			Enabled:         true,
			Path:            "/data/recordings",
			SegmentTemplate: "%path/%Y-%m-%d_%H-%M-%S",
			Format:          "fmp4",
			SegmentDuration: "1h",
			DeleteAfter:     "0s",
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return Config{}, err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c *Config) applyDefaults() {
	def := Default()
	if c.Server.Listen == "" {
		c.Server.Listen = def.Server.Listen
	}
	if c.Server.DataDir == "" {
		c.Server.DataDir = def.Server.DataDir
	}
	if c.Input.Path == "" {
		c.Input.Path = def.Input.Path
	}
	if c.MediaMTX.Binary == "" {
		c.MediaMTX.Binary = def.MediaMTX.Binary
	}
	if c.MediaMTX.ConfigPath == "" {
		c.MediaMTX.ConfigPath = def.MediaMTX.ConfigPath
	}
	if c.MediaMTX.APIURL == "" {
		c.MediaMTX.APIURL = def.MediaMTX.APIURL
	}
	if c.MediaMTX.RTSPURL == "" {
		c.MediaMTX.RTSPURL = def.MediaMTX.RTSPURL
	}
	if c.Recording.Path == "" {
		c.Recording.Path = def.Recording.Path
	}
	if c.Recording.SegmentTemplate == "" {
		c.Recording.SegmentTemplate = def.Recording.SegmentTemplate
	}
	if c.Recording.Format == "" {
		c.Recording.Format = def.Recording.Format
	}
	if c.Recording.SegmentDuration == "" {
		c.Recording.SegmentDuration = def.Recording.SegmentDuration
	}
	if c.Recording.DeleteAfter == "" {
		c.Recording.DeleteAfter = def.Recording.DeleteAfter
	}
	if c.Version == 0 {
		c.Version = Version
	}

	for i := range c.Outputs {
		if c.Outputs[i].ID == "" {
			c.Outputs[i].ID = fmt.Sprintf("output-%d", i+1)
		}
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Input.Path) == "" {
		return errors.New("input.path is required")
	}
	if c.Input.SRTLatencyMs < 0 {
		return errors.New("input.srt_latency_ms must be >= 0")
	}
	if c.Input.SRTLatencyMs > 0 && c.Input.SRTLatencyMs < 120 {
		return errors.New("input.srt_latency_ms must be at least 120 when set")
	}
	for _, out := range c.Outputs {
		if err := out.Validate(); err != nil {
			return fmt.Errorf("output %q: %w", out.ID, err)
		}
	}
	return nil
}

func (o Output) Validate() error {
	if strings.TrimSpace(o.Label) == "" {
		return errors.New("label is required")
	}
	if strings.TrimSpace(o.URL) == "" {
		return errors.New("url is required")
	}
	u, err := url.Parse(o.URL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "rtmp", "rtmps":
	default:
		return fmt.Errorf("url scheme must be rtmp or rtmps, got %q", u.Scheme)
	}
	if strings.TrimSpace(o.StreamKey) == "" {
		return errors.New("stream_key is required")
	}
	return nil
}

func (o Output) DestinationURL() string {
	base := strings.TrimRight(o.URL, "/")
	key := strings.TrimLeft(o.StreamKey, "/")
	return base + "/" + key
}

func (o Output) Redacted() Output {
	redacted := o
	redacted.StreamKey = RedactSecret(o.StreamKey)
	return redacted
}

func RedactSecret(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
}

func (c Config) Save(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	c.Version = Version

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := yaml.Marshal(&c)
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c Config) Redacted() Config {
	out := c
	out.Outputs = make([]Output, len(c.Outputs))
	for i, o := range c.Outputs {
		out.Outputs[i] = o.Redacted()
	}
	return out
}

func (c Config) InputRTSPURL() string {
	base := strings.TrimRight(c.MediaMTX.RTSPURL, "/")
	return base + "/" + c.Input.Path
}
