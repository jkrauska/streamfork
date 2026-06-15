package config

import "fmt"

// Default service ports — grouped in 87xx–89xx for easy recall:
//
//	8787  streamfork control API
//	8797  mediamtx control API (internal)
//	8798  mediamtx metrics (internal)
//	8854  RTSP preview  (88 + classic RTSP 554)
//	8890  SRT ingest
//	8935  RTMP          (88 + classic RTMP 1935)
const (
	PortControlAPI      = 8787
	PortMediaMTXAPI     = 8797
	PortMediaMTXMetrics = 8798
	PortRTSP            = 8854
	PortSRT             = 8890
	PortRTMP            = 8935
)

func DefaultMediaMTXAPIURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", PortMediaMTXAPI)
}

func DefaultRTSPURL() string {
	return fmt.Sprintf("rtsp://127.0.0.1:%d", PortRTSP)
}
