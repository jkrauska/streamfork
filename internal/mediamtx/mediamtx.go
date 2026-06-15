package mediamtx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"text/template"
	"time"

	"github.com/jkrauska/streamfork/internal/config"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

type PathList struct {
	Items []Path `json:"items"`
}

type Path struct {
	Name          string `json:"name"`
	Online        bool   `json:"online"`
	Available     bool   `json:"available"`
	InboundBytes  uint64 `json:"inboundBytes"`
	OutboundBytes uint64 `json:"outboundBytes"`
}

type SRTConnList struct {
	Items []SRTConn `json:"items"`
}

type SRTConn struct {
	ID                    string  `json:"id"`
	State                 string  `json:"state"`
	Path                  string  `json:"path"`
	RemoteAddr            string  `json:"remoteAddr"`
	MbpsReceiveRate       float64 `json:"mbpsReceiveRate"`
	MbpsSendRate          float64 `json:"mbpsSendRate"`
	MsRTT                 float64 `json:"msRTT"`
	MsReceiveBuf          uint64  `json:"msReceiveBuf"`
	MsReceiveTsbPdDelay   uint64  `json:"msReceiveTsbPdDelay"`
	PacketsReceivedLoss   uint64  `json:"packetsReceivedLoss"`
	PacketsReceivedRetrans uint64 `json:"packetsReceivedRetrans"`
	PacketsReceivedDrop   uint64  `json:"packetsReceivedDrop"`
	BytesReceived         uint64  `json:"bytesReceived"`
}

type InputStats struct {
	Online              bool    `json:"online"`
	Protocol            string  `json:"protocol,omitempty"`
	SourceIP            string  `json:"source_ip,omitempty"`
	BitrateMbps         float64 `json:"bitrate_mbps"`
	RTTMs               float64 `json:"rtt_ms"`
	ReceiveBufferMs     uint64  `json:"receive_buffer_ms"`
	ReceiveLatencyMs    uint64  `json:"receive_latency_ms"`
	PacketsReceivedLoss uint64  `json:"packets_received_loss"`
	PacketsRetrans      uint64  `json:"packets_retrans"`
	PacketsDropped      uint64  `json:"packets_dropped"`
	InboundBytes        uint64  `json:"inbound_bytes"`
	RemoteAddr          string  `json:"remote_addr,omitempty"`
}

type RTMPConnList struct {
	Items []RTMPConn `json:"items"`
}

type RTMPConn struct {
	State      string `json:"state"`
	Path       string `json:"path"`
	RemoteAddr string `json:"remoteAddr"`
}

type RTSPSessionList struct {
	Items []RTSPSession `json:"items"`
}

type RTSPSession struct {
	State      string `json:"state"`
	Path       string `json:"path"`
	RemoteAddr string `json:"remoteAddr"`
}

func (c *Client) GetPath(ctx context.Context, name string) (*Path, error) {
	var path Path
	if err := c.getJSON(ctx, fmt.Sprintf("/v3/paths/get/%s", name), &path); err != nil {
		return nil, err
	}
	return &path, nil
}

func (c *Client) Ready(ctx context.Context) error {
	var paths PathList
	return c.getJSON(ctx, "/v3/paths/list", &paths)
}

func (c *Client) PathConfigured(ctx context.Context, pathName string) error {
	_, err := c.GetPath(ctx, pathName)
	return err
}

func (c *Client) InputPublishing(ctx context.Context, pathName string) (bool, error) {
	stats, err := c.GetInputStats(ctx, pathName)
	if err != nil {
		return false, err
	}
	return stats.Online, nil
}

func (c *Client) GetInputStats(ctx context.Context, pathName string) (InputStats, error) {
	stats := InputStats{}

	path, err := c.GetPath(ctx, pathName)
	if err == nil {
		stats.Online = path.Online || path.Available
		stats.InboundBytes = path.InboundBytes
	}

	if pub := c.findSRTPublisher(ctx, pathName); pub != nil {
		applyPublisher(&stats, "SRT", pub.remoteAddr, pub.bytesReceived)
		stats.BitrateMbps = pub.bitrateMbps
		stats.RTTMs = pub.rttMs
		stats.ReceiveBufferMs = pub.receiveBufferMs
		stats.ReceiveLatencyMs = pub.receiveLatencyMs
		stats.PacketsReceivedLoss = pub.packetsLoss
		stats.PacketsRetrans = pub.packetsRetrans
		stats.PacketsDropped = pub.packetsDropped
		return stats, nil
	}

	if pub := c.findRTMPPublisher(ctx, pathName); pub != nil {
		applyPublisher(&stats, "RTMP", pub.remoteAddr, pub.bytesReceived)
		return stats, nil
	}

	if pub := c.findRTSPPublisher(ctx, pathName); pub != nil {
		applyPublisher(&stats, "RTSP", pub.remoteAddr, pub.bytesReceived)
		return stats, nil
	}

	return stats, nil
}

type publisherInfo struct {
	remoteAddr     string
	bytesReceived  uint64
	bitrateMbps    float64
	rttMs          float64
	receiveBufferMs uint64
	receiveLatencyMs uint64
	packetsLoss    uint64
	packetsRetrans uint64
	packetsDropped uint64
}

func applyPublisher(stats *InputStats, protocol, remoteAddr string, bytesReceived uint64) {
	stats.Online = true
	stats.Protocol = protocol
	stats.RemoteAddr = remoteAddr
	stats.SourceIP = sourceHost(remoteAddr)
	if bytesReceived > stats.InboundBytes {
		stats.InboundBytes = bytesReceived
	}
}

func sourceHost(addr string) string {
	if addr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func (c *Client) findSRTPublisher(ctx context.Context, pathName string) *publisherInfo {
	var conns SRTConnList
	if err := c.getJSON(ctx, "/v3/srtconns/list", &conns); err != nil {
		return nil
	}

	for _, conn := range conns.Items {
		if conn.Path != pathName || conn.State != "publish" {
			continue
		}
		return &publisherInfo{
			remoteAddr:       conn.RemoteAddr,
			bytesReceived:    conn.BytesReceived,
			bitrateMbps:      conn.MbpsReceiveRate,
			rttMs:            conn.MsRTT,
			receiveBufferMs:  conn.MsReceiveBuf,
			receiveLatencyMs: conn.MsReceiveTsbPdDelay,
			packetsLoss:      conn.PacketsReceivedLoss,
			packetsRetrans:   conn.PacketsReceivedRetrans,
			packetsDropped:   conn.PacketsReceivedDrop,
		}
	}
	return nil
}

func (c *Client) findRTMPPublisher(ctx context.Context, pathName string) *publisherInfo {
	var conns RTMPConnList
	if err := c.getJSON(ctx, "/v3/rtmpconns/list", &conns); err != nil {
		return nil
	}

	for _, conn := range conns.Items {
		if conn.Path != pathName || conn.State != "publish" {
			continue
		}
		return &publisherInfo{remoteAddr: conn.RemoteAddr}
	}
	return nil
}

func (c *Client) findRTSPPublisher(ctx context.Context, pathName string) *publisherInfo {
	var sessions RTSPSessionList
	if err := c.getJSON(ctx, "/v3/rtspsessions/list", &sessions); err != nil {
		return nil
	}

	for _, session := range sessions.Items {
		if session.Path != pathName || session.State != "publish" {
			continue
		}
		return &publisherInfo{remoteAddr: session.RemoteAddr}
	}
	return nil
}

func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("mediamtx api %s: %s", path, resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(dest)
}

type RenderParams struct {
	InputPath            string
	SRTPublishPassphrase string
	RecordingEnabled     bool
	RecordPath           string
	RecordFormat         string
	SegmentDuration      string
	DeleteAfter          string
	MediaMTXAPIPort      int
	MediaMTXMetricsPort  int
	RTSPPort             int
	RTMPPort             int
	SRTPort              int
}

func RenderConfig(cfg config.Config) ([]byte, error) {
	tmpl, err := template.New("mediamtx").Funcs(template.FuncMap{
		"quote": strconv.Quote,
		"record": func(enabled bool) string {
			if enabled {
				return "yes"
			}
			return "no"
		},
	}).Parse(mediaMTXTemplate)
	if err != nil {
		return nil, err
	}

	recordPath := filepath.Join(cfg.Recording.Path, cfg.Recording.SegmentTemplate)
	params := RenderParams{
		InputPath:            cfg.Input.Path,
		SRTPublishPassphrase: cfg.Input.SRTPublishPassphrase,
		RecordingEnabled:     cfg.Recording.Enabled,
		RecordPath:           recordPath,
		RecordFormat:         cfg.Recording.Format,
		SegmentDuration:      cfg.Recording.SegmentDuration,
		DeleteAfter:          cfg.Recording.DeleteAfter,
		MediaMTXAPIPort:      config.PortMediaMTXAPI,
		MediaMTXMetricsPort:  config.PortMediaMTXMetrics,
		RTSPPort:             config.PortRTSP,
		RTMPPort:             config.PortRTMP,
		SRTPort:              config.PortSRT,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func WriteConfig(cfg config.Config) error {
	data, err := RenderConfig(cfg)
	if err != nil {
		return err
	}

	path := cfg.MediaMTX.ConfigPath
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

const mediaMTXTemplate = `# Generated by streamfork — do not edit by hand.
logLevel: info
logDestinations: [stdout]

api: yes
apiAddress: 127.0.0.1:{{ .MediaMTXAPIPort }}

metrics: yes
metricsAddress: 127.0.0.1:{{ .MediaMTXMetricsPort }}

rtsp: yes
rtspAddress: :{{ .RTSPPort }}

rtmp: yes
rtmpAddress: :{{ .RTMPPort }}

srt: yes
srtAddress: :{{ .SRTPort }}

pathDefaults:
  source: publisher
  overridePublisher: yes

paths:
  {{ .InputPath }}:
    source: publisher
    srtPublishPassphrase: {{ quote .SRTPublishPassphrase }}
    record: {{ record .RecordingEnabled }}
    recordPath: {{ quote .RecordPath }}
    recordFormat: {{ quote .RecordFormat }}
    recordSegmentDuration: {{ quote .SegmentDuration }}
    recordDeleteAfter: {{ quote .DeleteAfter }}
`
