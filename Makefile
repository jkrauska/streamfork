BINARY := bin/streamfork
CONFIG ?= data/streamfork.yml
LOCAL_CONFIG := data/streamfork.local.yml

.PHONY: build run run-local run-linux setup setup-local deps-macos

build:
	@mkdir -p bin
	go build -trimpath -o $(BINARY) ./cmd/streamfork

setup:
	@mkdir -p data/recordings
	@test -f $(CONFIG) || cp configs/streamfork.example.yml $(CONFIG)

setup-local:
	@mkdir -p data/recordings bin
	@test -f $(LOCAL_CONFIG) || cp configs/streamfork.local.yml $(LOCAL_CONFIG)

deps-macos:
	@./scripts/install-macos-deps.sh

run: build setup
	docker compose up --build

run-linux: build setup
	docker compose -f docker-compose.linux.yml up --build

# Native macOS — no Docker; SRT binds directly on :8890 (works with Starlink/WAN)
run-local: build setup-local
	@test -x bin/mediamtx || { echo "Missing bin/mediamtx — run: make deps-macos"; exit 1; }
	@command -v ffmpeg >/dev/null || { echo "Missing ffmpeg — run: make deps-macos"; exit 1; }
	STREAMFORK_CONFIG=$(LOCAL_CONFIG) ./$(BINARY)
