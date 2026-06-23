package api

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jkrauska/streamfork/internal/config"
	"github.com/jkrauska/streamfork/internal/mediamtx"
	"github.com/jkrauska/streamfork/internal/supervisor"
)

//go:embed static/*
var staticFiles embed.FS

type ConfigStore interface {
	Get() config.Config
	Save(cfg config.Config) error
	ConfigPath() string
}

type Runtime interface {
	StartOutput(id string) error
	StopOutput(id string) error
	RestartOutput(id string) error
	ReloadOutputs(outputs []config.Output)
	OutputStatuses() []supervisor.OutputStatus
	MediaRunning() bool
}

type Server struct {
	store    ConfigStore
	runtime  Runtime
	mtx      *mediamtx.Client
	snapshot SnapshotProvider
	mux      *http.ServeMux
}

type SnapshotProvider interface {
	Image() ([]byte, time.Time, bool)
	VideoStats() (width, height int, fps float64, ok bool)
}

func NewServer(store ConfigStore, runtime Runtime, mtx *mediamtx.Client, snapshot SnapshotProvider) *Server {
	s := &Server{
		store:    store,
		runtime:  runtime,
		mtx:      mtx,
		snapshot: snapshot,
		mux:      http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	ui, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic("api static files: " + err.Error())
	}
	s.mux.Handle("GET /{$}", http.FileServer(http.FS(ui)))

	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /api/status", s.handleStatus)
	s.mux.HandleFunc("GET /api/input/snapshot.jpg", s.handleInputSnapshot)
	s.mux.HandleFunc("GET /api/outputs", s.handleListOutputs)
	s.mux.HandleFunc("POST /api/outputs", s.handleCreateOutput)
	s.mux.HandleFunc("PUT /api/outputs/{id}", s.handleUpdateOutput)
	s.mux.HandleFunc("DELETE /api/outputs/{id}", s.handleDeleteOutput)
	s.mux.HandleFunc("POST /api/outputs/{id}/start", s.handleOutputAction(s.runtime.StartOutput))
	s.mux.HandleFunc("POST /api/outputs/{id}/stop", s.handleOutputAction(s.runtime.StopOutput))
	s.mux.HandleFunc("POST /api/outputs/{id}/restart", s.handleOutputAction(s.runtime.RestartOutput))
}

type StatusResponse struct {
	Now        time.Time                `json:"now"`
	Input      mediamtx.InputStats      `json:"input"`
	InputSettings InputSettings         `json:"input_settings"`
	MediaMTX   MediaMTXStatus           `json:"mediamtx"`
	Outputs    []supervisor.OutputStatus `json:"outputs"`
	Recording  RecordingStatus          `json:"recording"`
}

type InputSettings struct {
	Path         string `json:"path"`
	SRTLatencyMs int    `json:"srt_latency_ms,omitempty"`
}

type MediaMTXStatus struct {
	Running bool `json:"running"`
}

type RecordingStatus struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Get()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	input, _ := s.mtx.GetInputStats(ctx, cfg.Input.Path)
	if s.snapshot != nil {
		if width, height, fps, ok := s.snapshot.VideoStats(); ok {
			input.VideoWidth = width
			input.VideoHeight = height
			input.VideoFPS = fps
		}
	}
	resp := StatusResponse{
		Now:     time.Now().UTC(),
		Input:   input,
		InputSettings: InputSettings{
			Path:         cfg.Input.Path,
			SRTLatencyMs: cfg.Input.SRTLatencyMs,
		},
		MediaMTX: MediaMTXStatus{Running: s.runtime.MediaRunning()},
		Outputs: s.runtime.OutputStatuses(),
		Recording: RecordingStatus{
			Enabled: cfg.Recording.Enabled,
			Path:    cfg.Recording.Path,
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleInputSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.snapshot == nil {
		http.NotFound(w, r)
		return
	}

	image, capturedAt, ok := s.snapshot.Image()
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Snapshot-Time", capturedAt.Format(time.RFC3339))
	_, _ = w.Write(image)
}

func (s *Server) handleListOutputs(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Get()
	writeJSON(w, http.StatusOK, cfg.Outputs)
}

func (s *Server) handleCreateOutput(w http.ResponseWriter, r *http.Request) {
	var out config.Output
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	cfg := s.store.Get()
	if out.ID == "" {
		out.ID = nextOutputID(cfg.Outputs)
	}
	if err := out.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	for _, existing := range cfg.Outputs {
		if existing.ID == out.ID {
			writeError(w, http.StatusConflict, errors.New("output id already exists"))
			return
		}
	}

	cfg.Outputs = append(cfg.Outputs, out)
	if err := s.persistAndReload(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleUpdateOutput(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var patch outputPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	cfg := s.store.Get()
	idx := -1
	for i, out := range cfg.Outputs {
		if out.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		writeError(w, http.StatusNotFound, errors.New("output not found"))
		return
	}

	updated := cfg.Outputs[idx]
	if patch.Label != nil {
		updated.Label = *patch.Label
	}
	if patch.URL != nil {
		updated.URL = *patch.URL
	}
	if patch.StreamKey != nil && !strings.HasPrefix(*patch.StreamKey, "*") {
		updated.StreamKey = *patch.StreamKey
	}
	if patch.Enabled != nil {
		updated.Enabled = *patch.Enabled
	}
	if patch.TranscodeH264 != nil {
		updated.TranscodeH264 = *patch.TranscodeH264
	}

	if err := updated.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	cfg.Outputs[idx] = updated
	if err := s.persistAndReload(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteOutput(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg := s.store.Get()

	next := make([]config.Output, 0, len(cfg.Outputs))
	found := false
	for _, out := range cfg.Outputs {
		if out.ID == id {
			found = true
			continue
		}
		next = append(next, out)
	}
	if !found {
		writeError(w, http.StatusNotFound, errors.New("output not found"))
		return
	}

	cfg.Outputs = next
	if err := s.persistAndReload(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOutputAction(fn func(string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := fn(id); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (s *Server) persistAndReload(cfg config.Config) error {
	if err := s.store.Save(cfg); err != nil {
		return err
	}
	s.runtime.ReloadOutputs(cfg.Outputs)
	return nil
}

func nextOutputID(outputs []config.Output) string {
	max := 0
	for _, out := range outputs {
		if n, err := strconv.Atoi(strings.TrimPrefix(out.ID, "output-")); err == nil && n > max {
			max = n
		}
	}
	return "output-" + strconv.Itoa(max+1)
}

type outputPatch struct {
	Label         *string `json:"label"`
	URL           *string `json:"url"`
	StreamKey     *string `json:"stream_key"`
	Enabled       *bool   `json:"enabled"`
	TranscodeH264 *bool   `json:"transcode_h264"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
