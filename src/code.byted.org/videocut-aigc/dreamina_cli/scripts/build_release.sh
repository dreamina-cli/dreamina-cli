#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPO_ROOT="$(cd "${PROJECT_DIR}/../../../../" && pwd)"
MODULE_DIR="${PROJECT_DIR}/src"
DIST_DIR="${PROJECT_DIR}/dist"
PACKAGE_DIR="${DIST_DIR}/packages"

VERSION="${VERSION:-$(git -C "${REPO_ROOT}" describe --always --dirty 2>/dev/null || date +%Y%m%d%H%M%S)}"
COMMIT="${COMMIT:-$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo unknown)}"
BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

compute_sha256() {
  local path="$1"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${path}" | awk '{print $1}'
    return
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${path}" | awk '{print $1}'
    return
  fi
  python3 - "${path}" <<'PY'
import hashlib
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
print(hashlib.sha256(path.read_bytes()).hexdigest())
PY
}

package_release() {
  local goos="$1"
  local goarch="$2"
  local out_bin="$3"
  local archive_base="dreamina-${VERSION}-${goos}-${goarch}"
  local stage_dir="${PACKAGE_DIR}/${archive_base}"
  local archive_path

  rm -rf "${stage_dir}"
  mkdir -p "${stage_dir}"
  cp "${PROJECT_DIR}/README.md" "${stage_dir}/README.md"
  cp "${out_bin}" "${stage_dir}/$(basename "${out_bin}")"

  if [[ "${goos}" == "windows" ]]; then
    archive_path="${PACKAGE_DIR}/${archive_base}.zip"
    rm -f "${archive_path}"
    python3 - "${stage_dir}" "${archive_path}" <<'PY'
import pathlib
import sys
import zipfile

stage = pathlib.Path(sys.argv[1])
archive = pathlib.Path(sys.argv[2])
with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as zf:
    for path in stage.rglob("*"):
        if path.is_file():
            zf.write(path, path.relative_to(stage.parent))
PY
  else
    archive_path="${PACKAGE_DIR}/${archive_base}.tar.gz"
    rm -f "${archive_path}"
    tar -C "${PACKAGE_DIR}" -czf "${archive_path}" "${archive_base}"
  fi

  printf '%s  %s\n' "$(compute_sha256 "${archive_path}")" "$(basename "${archive_path}")" >> "${PACKAGE_DIR}/SHA256SUMS.txt"
  rm -rf "${stage_dir}"
}

declare -a TARGETS
if [[ "$#" -gt 0 ]]; then
  TARGETS=("$@")
else
  TARGETS=(
    "darwin/arm64"
    "darwin/amd64"
    "linux/amd64"
    "linux/arm64"
    "windows/amd64"
    "windows/arm64"
  )
fi

mkdir -p "${DIST_DIR}" "${PACKAGE_DIR}"
rm -f "${PACKAGE_DIR}/SHA256SUMS.txt"

host_goos="$(go env GOOS)"
host_goarch="$(go env GOARCH)"

for target in "${TARGETS[@]}"; do
  goos="${target%%/*}"
  goarch="${target##*/}"
  if [[ -z "${goos}" || -z "${goarch}" || "${goos}" == "${goarch}" ]]; then
    echo "invalid target: ${target}, expected GOOS/GOARCH" >&2
    exit 1
  fi

  out_dir="${DIST_DIR}/${goos}-${goarch}"
  out_bin="${out_dir}/dreamina"
  if [[ "${goos}" == "windows" ]]; then
    out_bin="${out_bin}.exe"
  fi

  mkdir -p "${out_dir}"

  echo "==> building ${goos}/${goarch}"
  (
    cd "${MODULE_DIR}"
    GOOS="${goos}" GOARCH="${goarch}" CGO_ENABLED=0 \
      go build \
      -ldflags "-X code.byted.org/videocut-aigc/dreamina_cli/buildinfo.Version=${VERSION} -X code.byted.org/videocut-aigc/dreamina_cli/buildinfo.Commit=${COMMIT} -X code.byted.org/videocut-aigc/dreamina_cli/buildinfo.BuildTime=${BUILD_TIME}" \
      -o "${out_bin}" \
      .
  )

  package_release "${goos}" "${goarch}" "${out_bin}"

  if [[ "${goos}" == "${host_goos}" && "${goarch}" == "${host_goarch}" && "${goos}" != "windows" ]]; then
    mkdir -p "${DIST_DIR}/current"
    cp "${out_bin}" "${DIST_DIR}/current/dreamina"
    chmod +x "${DIST_DIR}/current/dreamina"
  fi
done

echo "build completed: ${DIST_DIR}"
