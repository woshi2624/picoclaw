#!/usr/bin/env bash
set -euo pipefail

# ── Configuration ────────────────────────────────────────────────────
PROJECT_NAME="picoclaw"
BUILD_DIR="build"
CMD_DIR="cmd/${PROJECT_NAME}"
WEB_BACKEND_DIR="web/backend"
WEB_FRONTEND_DIR="web/frontend"

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
GIT_COMMIT="$(git rev-parse --short=8 HEAD 2>/dev/null || echo "dev")"
BUILD_TIME="$(date +%FT%T%z)"
GO_VERSION="$(go version | awk '{print $3}')"

CONFIG_PKG="github.com/sipeed/picoclaw/pkg/config"
LDFLAGS="-s -w \
  -X ${CONFIG_PKG}.Version=${VERSION} \
  -X ${CONFIG_PKG}.GitCommit=${GIT_COMMIT} \
  -X ${CONFIG_PKG}.BuildTime=${BUILD_TIME} \
  -X ${CONFIG_PKG}.GoVersion=${GO_VERSION}"

export CGO_ENABLED=0
GO_TAGS="stdjson"

# ── Target platforms ─────────────────────────────────────────────────
# Format: GOOS:GOARCH[:GOARM[:GOMIPS]]
DEFAULT_TARGETS=(
  "linux:amd64"
  "linux:arm64"
  "linux:arm:7"
  "darwin:arm64"
  "darwin:amd64"
  "windows:amd64"
)

# ── Helper functions ─────────────────────────────────────────────────
info()  { printf "\033[1;34m==>\033[0m %s\n" "$*"; }
ok()    { printf "\033[1;32m==>\033[0m %s\n" "$*"; }
err()   { printf "\033[1;31m==>\033[0m %s\n" "$*" >&2; }

binary_name() {
  local goos=$1 goarch=$2 name=$3
  local suffix=""
  [[ "$goos" == "windows" ]] && suffix=".exe"
  echo "${name}-${goos}-${goarch}${suffix}"
}

build_go() {
  local goos=$1 goarch=$2 main_pkg=$3 out=$4
  shift 4

  info "Building ${out} (${goos}/${goarch})..."
  env GOOS="$goos" GOARCH="$goarch" "$@" \
    go build -tags "$GO_TAGS" -ldflags "$LDFLAGS" -o "${BUILD_DIR}/${out}" "./${main_pkg}"
}

# ── Commands ─────────────────────────────────────────────────────────

cmd_frontend() {
  info "Building frontend..."
  if ! command -v pnpm &>/dev/null; then
    err "pnpm not found. Install with: npm install -g pnpm"
    exit 1
  fi
  (cd "$WEB_FRONTEND_DIR" && pnpm install --frozen-lockfile && pnpm build:backend)
  ok "Frontend built → ${WEB_BACKEND_DIR}/dist/"
}

cmd_generate() {
  info "Running go generate..."
  rm -rf "./${CMD_DIR}/workspace" 2>/dev/null || true
  go generate ./...
}

cmd_package() {
  local goos=$1 goarch=$2 agent_bin=$3 launcher_bin=$4
  local suffix=""
  [[ "$goos" == "windows" ]] && suffix=".exe"

  local pkg_name="${PROJECT_NAME}-${goos}-${goarch}"
  local stage_dir="${BUILD_DIR}/.stage"

  info "Packaging ${pkg_name}.zip..."
  rm -rf "${stage_dir}/${pkg_name}"
  mkdir -p "${stage_dir}/${pkg_name}"
  cp "${BUILD_DIR}/${agent_bin}" "${stage_dir}/${pkg_name}/${PROJECT_NAME}${suffix}"
  cp "${BUILD_DIR}/${launcher_bin}" "${stage_dir}/${pkg_name}/${PROJECT_NAME}-launcher${suffix}"
  (cd "${stage_dir}" && zip -r "../${pkg_name}.zip" "${pkg_name}/")
  rm -rf "${stage_dir}/${pkg_name}"
  ok "Packaged: ${BUILD_DIR}/${pkg_name}.zip"
}

cmd_build() {
  local targets=("${@:-}")
  [[ ${#targets[@]} -eq 0 || -z "${targets[0]}" ]] && targets=("${DEFAULT_TARGETS[@]}")

  cmd_clean
  cmd_generate

  cmd_frontend

  mkdir -p "$BUILD_DIR"

  for target in "${targets[@]}"; do
    IFS=: read -r goos goarch goarm gomips <<< "$target"
    local extra_env=()
    [[ -n "${goarm:-}" ]]  && extra_env+=("GOARM=$goarm")
    [[ -n "${gomips:-}" ]] && extra_env+=("GOMIPS=$gomips")

    # picoclaw agent
    local agent_bin
    agent_bin="$(binary_name "$goos" "$goarch" "$PROJECT_NAME")"
    build_go "$goos" "$goarch" "$CMD_DIR" "$agent_bin" ${extra_env[@]+"${extra_env[@]}"}

    # picoclaw-launcher (web console)
    local launcher_bin
    launcher_bin="$(binary_name "$goos" "$goarch" "${PROJECT_NAME}-launcher")"
    build_go "$goos" "$goarch" "$WEB_BACKEND_DIR" "$launcher_bin" ${extra_env[@]+"${extra_env[@]}"}

    # Create short-name symlinks so launcher can find the agent binary
    local suffix=""
    [[ "$goos" == "windows" ]] && suffix=".exe"
    (cd "$BUILD_DIR" && ln -sf "$agent_bin" "${PROJECT_NAME}${suffix}")
    (cd "$BUILD_DIR" && ln -sf "$launcher_bin" "${PROJECT_NAME}-launcher${suffix}")

    # Package platform binaries into zip archive
    cmd_package "$goos" "$goarch" "$agent_bin" "$launcher_bin"
  done

  rm -rf "${BUILD_DIR}/.stage"

  # Remove all non-zip files (binaries, symlinks, etc.)
  find "${BUILD_DIR}" -maxdepth 1 ! -name "*.zip" -not -type d -delete

  ok "Build complete. Zip archives in ${BUILD_DIR}/:"
  ls -lh "${BUILD_DIR}/"*.zip 2>/dev/null | grep -v "^total" || true
}

cmd_windows() {
  info "Building for Windows (amd64)..."
  cmd_build "windows:amd64"
}

cmd_clean() {
  info "Cleaning ${BUILD_DIR}/..."
  rm -rf "$BUILD_DIR"
  ok "Clean"
}

cmd_help() {
  cat <<EOF
Usage: $0 <command> [targets...]

Commands:
  build [targets]   Build agent + launcher for given targets (default: all)
  windows           Build for Windows amd64 only
  frontend          Build frontend only
  clean             Remove build artifacts
  help              Show this message

Targets:
  Format: GOOS:GOARCH[:GOARM[:GOMIPS]]
  Default targets:
$(printf '    %s\n' "${DEFAULT_TARGETS[@]}")

Examples:
  $0 build                        # Build all default targets
  $0 windows                      # Build Windows only
  $0 build windows:amd64          # Same as above
  $0 build linux:amd64 darwin:arm64  # Build specific targets

Version: ${VERSION}
EOF
}

# ── Entry point ──────────────────────────────────────────────────────
cmd="${1:-help}"
shift || true

case "$cmd" in
  build)    cmd_build "$@" ;;
  windows)  cmd_windows ;;
  frontend) cmd_frontend ;;
  clean)    cmd_clean ;;
  help|*)   cmd_help ;;
esac
