#!/usr/bin/env bash
# Install native dependencies for `make run-local` on macOS.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${ROOT}/bin"
MEDIAMTX_VERSION="${MEDIAMTX_VERSION:-1.11.3}"

mkdir -p "$BIN"

if ! command -v brew >/dev/null 2>&1; then
  echo "Homebrew is required: https://brew.sh"
  exit 1
fi

ffmpeg_ok() {
  command -v ffmpeg >/dev/null 2>&1 && ffmpeg -version >/dev/null 2>&1
}

fix_ffmpeg() {
  echo "Repairing ffmpeg (reinstalling x264 + ffmpeg)..."
  if brew reinstall x264 ffmpeg 2>/dev/null; then
    return 0
  fi
  echo "brew reinstall failed; trying clean install..."
  brew uninstall --force ffmpeg 2>/dev/null || true
  brew install ffmpeg
}

if ! ffmpeg_ok; then
  if command -v ffmpeg >/dev/null 2>&1; then
    echo "ffmpeg is installed but broken (common after x264 upgrades)."
    fix_ffmpeg
  else
    echo "Installing ffmpeg..."
    brew install ffmpeg
  fi
fi

if ! ffmpeg_ok; then
  echo "ffmpeg still not working. Try manually:"
  echo "  brew uninstall --force ffmpeg && brew install ffmpeg"
  exit 1
fi

echo "ffmpeg: $(ffmpeg -version 2>/dev/null | head -1)"

case "$(uname -m)" in
  arm64) MTX_ARCH=darwin_arm64 ;;
  x86_64) MTX_ARCH=darwin_amd64 ;;
  *)
    echo "Unsupported macOS arch: $(uname -m)"
    exit 1
    ;;
esac

MTX_TAR="mediamtx_v${MEDIAMTX_VERSION}_${MTX_ARCH}.tar.gz"
MTX_URL="https://github.com/bluenviron/mediamtx/releases/download/v${MEDIAMTX_VERSION}/${MTX_TAR}"

if [[ ! -x "${BIN}/mediamtx" ]]; then
  echo "Downloading MediaMTX ${MEDIAMTX_VERSION} (${MTX_ARCH})..."
  curl -fsSL "$MTX_URL" -o "/tmp/${MTX_TAR}"
  tar -xzf "/tmp/${MTX_TAR}" -C "$BIN" mediamtx
  chmod +x "${BIN}/mediamtx"
  rm -f "/tmp/${MTX_TAR}"
else
  echo "mediamtx: OK (${BIN}/mediamtx)"
fi

echo ""
echo "Ready for native run:"
echo "  cd ${ROOT} && make run-local"
