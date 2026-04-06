#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG_ROOT="${ROOT_DIR}/logs/smoke"
RUN_ID="$(date +%Y%m%d-%H%M%S)"
RUN_DIR="${LOG_ROOT}/${RUN_ID}"
RESULTS_MD="${RUN_DIR}/results.md"

: "${BASE_URL:=https://jimeng.jianying.com}"
: "${CLI_VERSION:=__YOUR_CLI_VERSION__}"
: "${COOKIE:=}"
: "${LOGID:=$(date +%Y%m%d%H%M%S)-smoke}"
: "${SUBMIT_ID:=__YOUR_SUBMIT_ID__}"
: "${RESOURCE_ID:=__YOUR_RESOURCE_ID__}"
: "${VIDEO_RESOURCE_ID:=__YOUR_VIDEO_RESOURCE_ID__}"
: "${AUDIO_RESOURCE_ID:=__YOUR_AUDIO_RESOURCE_ID__}"
: "${MULTIMODAL_SUBMIT_ID:=__YOUR_MULTIMODAL_SUBMIT_ID__}"
: "${FIRST_RESOURCE_ID:=__YOUR_FIRST_RESOURCE_ID__}"
: "${LAST_RESOURCE_ID:=__YOUR_LAST_RESOURCE_ID__}"
: "${IMAGE_PATH:=}"
: "${UPSCALE_RESOLUTION:=4k}"
: "${TEXT2VIDEO_MODEL_VERSION:=seedance2.0fast}"
: "${MULTIMODAL_MODEL_VERSION:=seedance2.0fast}"

SESSION_JSON_CACHE=""

is_placeholder_value() {
  local value="${1:-}"
  [[ -z "${value}" || "${value}" == __YOUR_*__ ]]
}

usage() {
  cat <<'EOF'
Usage:
  bash scripts/dreamina_smoke.sh plan
  bash scripts/dreamina_smoke.sh cli:user_credit
  bash scripts/dreamina_smoke.sh cli:text2video
  bash scripts/dreamina_smoke.sh cli:image2image
  bash scripts/dreamina_smoke.sh cli:image_upscale
  bash scripts/dreamina_smoke.sh cli:image_upscale_debug
  bash scripts/dreamina_smoke.sh cli:multimodal2video
  bash scripts/dreamina_smoke.sh cli:query_image2image
  bash scripts/dreamina_smoke.sh cli:query_image_upscale
  bash scripts/dreamina_smoke.sh cli:download_image_upscale
  bash scripts/dreamina_smoke.sh batch:core
  bash scripts/dreamina_smoke.sh batch:image-core
  bash scripts/dreamina_smoke.sh batch:image-upload-core
  bash scripts/dreamina_smoke.sh batch:video-upload-core
  bash scripts/dreamina_smoke.sh batch:image-result-core
  bash scripts/dreamina_smoke.sh batch:curl-core
  bash scripts/dreamina_smoke.sh batch:all
  bash scripts/dreamina_smoke.sh curl:user_credit
  bash scripts/dreamina_smoke.sh curl:query_result
  bash scripts/dreamina_smoke.sh curl:text2image
  bash scripts/dreamina_smoke.sh curl:text2video
  bash scripts/dreamina_smoke.sh curl:image2image
  bash scripts/dreamina_smoke.sh curl:image_upscale
  bash scripts/dreamina_smoke.sh curl:image2video
  bash scripts/dreamina_smoke.sh curl:frames2video
  bash scripts/dreamina_smoke.sh curl:multiframe2video
  bash scripts/dreamina_smoke.sh curl:ref2video
  bash scripts/dreamina_smoke.sh curl:multimodal2video

Environment:
  BASE_URL     default: https://jimeng.jianying.com
  CLI_VERSION  required by MCP/history requests
  COOKIE       required by all remote requests
  LOGID        optional; defaults to timestamp-based value
  SUBMIT_ID    required by query_result
  RESOURCE_ID  required by image/video payload examples
  VIDEO_RESOURCE_ID optional; overrides auto-resolved multimodal video resource id
  AUDIO_RESOURCE_ID optional; overrides auto-resolved multimodal audio resource id
  MULTIMODAL_SUBMIT_ID optional; used to auto-query resources for curl:multimodal2video
  FIRST_RESOURCE_ID optional; defaults to latest cli:image2image upload
  LAST_RESOURCE_ID optional; defaults to latest cli:image_upscale upload
  IMAGE_PATH   optional; defaults to ./testdata/smoke/image-1.png when present
  UPSCALE_RESOLUTION optional; defaults to 4k
  TEXT2VIDEO_MODEL_VERSION optional; defaults to seedance2.0fast, can be set to seedance2.0_vip or seedance2.0fast_vip
  MULTIMODAL_MODEL_VERSION optional; defaults to seedance2.0fast, can be set to seedance2.0_vip or seedance2.0fast_vip

Notes:
  - This script does not automate browser login.
  - Run `plan` first to see the recommended verification order.
  - For commands that need local files, prefer running the CLI examples first.
  - Non-plan runs write logs under logs/smoke/<timestamp>/.
EOF
}

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" || "${!name}" == __YOUR_*__ ]]; then
    echo "missing required env: ${name}" >&2
    return 1
  fi
}

require_envs() {
  local name
  local failed=0
  for name in "$@"; do
    if ! require_env "${name}"; then
      failed=1
    fi
  done
  return "${failed}"
}

resolve_dreamina_bin() {
  local goos goarch
  if command -v go >/dev/null 2>&1; then
    goos="$(go env GOOS 2>/dev/null || true)"
    goarch="$(go env GOARCH 2>/dev/null || true)"
    if [[ -n "${goos}" && -n "${goarch}" && -x "${ROOT_DIR}/dist/${goos}-${goarch}/dreamina" ]]; then
      echo "${ROOT_DIR}/dist/${goos}-${goarch}/dreamina"
      return
    fi
    if [[ -x "${ROOT_DIR}/dist/current/dreamina" ]]; then
      echo "${ROOT_DIR}/dist/current/dreamina"
      return
    fi
  fi
  if [[ -x "${ROOT_DIR}/bin/dreamina" ]]; then
    echo "${ROOT_DIR}/bin/dreamina"
    return
  fi
  if command -v dreamina >/dev/null 2>&1; then
    command -v dreamina
    return
  fi
  echo "dreamina binary not found; build ${ROOT_DIR}/dist/<goos>-<goarch>/dreamina or add dreamina to PATH" >&2
  return 1
}

resolve_session_json() {
  if [[ -n "${SESSION_JSON_CACHE}" ]]; then
    printf '%s' "${SESSION_JSON_CACHE}"
    return
  fi
  local bin
  bin="$(resolve_dreamina_bin)"
  SESSION_JSON_CACHE="$("${bin}" validate-auth-token)"
  printf '%s' "${SESSION_JSON_CACHE}"
}

resolve_session_field() {
  local mode="$1"
  local key="${2:-}"
  local session_json
  session_json="$(resolve_session_json)"
  SESSION_JSON="${session_json}" python3 - "${mode}" "${key}" <<'PY'
import json
import os
import sys

payload = json.loads(os.environ["SESSION_JSON"])
mode = sys.argv[1]
key = sys.argv[2] if len(sys.argv) > 2 else ""

if mode == "cookie":
    print(payload.get("cookie", ""))
elif mode == "header":
    headers = payload.get("headers", {}) or {}
    print(headers.get(key, ""))
PY
}

resolve_cookie() {
  if ! is_placeholder_value "${COOKIE:-}"; then
    printf '%s' "${COOKIE}"
    return
  fi
  resolve_session_field cookie
}

resolve_cli_version() {
  if ! is_placeholder_value "${CLI_VERSION:-}"; then
    printf '%s' "${CLI_VERSION}"
    return
  fi
  local bin
  bin="$(resolve_dreamina_bin)"
  "${bin}" version | python3 -c 'import sys, json; print(json.load(sys.stdin).get("version",""))'
}

resolve_submit_id() {
  if ! is_placeholder_value "${SUBMIT_ID:-}"; then
    printf '%s' "${SUBMIT_ID}"
    return
  fi
  local latest_log=""
  latest_log="$(ls -1t "${ROOT_DIR}"/logs/smoke/*/cli_text2video.stdout.log 2>/dev/null | head -n 1 || true)"
  if [[ -n "${latest_log}" && -f "${latest_log}" ]]; then
    python3 - "${latest_log}" <<'PY'
import json
import sys
from pathlib import Path

path = Path(sys.argv[1])
try:
    payload = json.loads(path.read_text(encoding="utf-8"))
except Exception:
    print("")
    raise SystemExit(0)
print(payload.get("submit_id", ""))
PY
    return
  fi
  echo "unable to resolve SUBMIT_ID automatically; set SUBMIT_ID or run cli:text2video first" >&2
  return 1
}

resolve_latest_submit_id_from_db() {
  local gen_task_type="$1"
  sqlite3 ~/.dreamina_cli/tasks.db "select submit_id from aigc_task where gen_task_type='${gen_task_type}' order by create_time desc limit 1;" 2>/dev/null || true
}

resolve_submit_id_from_case_log() {
  local stem="$1"
  local latest_log=""
  latest_log="$(ls -1t "${ROOT_DIR}"/logs/smoke/*/"${stem}".stdout.log 2>/dev/null | head -n 1 || true)"
  if [[ -n "${latest_log}" && -f "${latest_log}" ]]; then
    python3 - "${latest_log}" <<'PY'
import json
import sys
from pathlib import Path

path = Path(sys.argv[1])
try:
    payload = json.loads(path.read_text(encoding="utf-8"))
except Exception:
    print("")
    raise SystemExit(0)
print(payload.get("submit_id", ""))
PY
    return
  fi
  echo "unable to resolve submit_id from ${stem}; run the corresponding CLI smoke first" >&2
  return 1
}

resolve_multimodal_submit_id() {
  local submit_id=""
  if ! is_placeholder_value "${MULTIMODAL_SUBMIT_ID:-}"; then
    printf '%s' "${MULTIMODAL_SUBMIT_ID}"
    return
  fi
  submit_id="$(resolve_submit_id_from_case_log "cli_multimodal2video" || true)"
  if [[ -n "${submit_id}" ]]; then
    printf '%s' "${submit_id}"
    return
  fi
  submit_id="$(resolve_latest_submit_id_from_db "multimodal2video")"
  if [[ -n "${submit_id}" ]]; then
    printf '%s' "${submit_id}"
    return
  fi
  echo "unable to resolve multimodal submit_id automatically; set MULTIMODAL_SUBMIT_ID or run cli:multimodal2video first" >&2
  return 1
}

resolve_resource_id_from_submit() {
  local submit_id="$1"
  [[ -n "${submit_id}" ]] || return 1
  local body
  body="$(sqlite3 ~/.dreamina_cli/tasks.db "select result_json from aigc_task where submit_id='${submit_id}' limit 1;")"
  RESULT_JSON="${body}" python3 - <<'PY'
import json
import os

body = os.environ.get("RESULT_JSON", "").strip()
if not body:
    print("")
    raise SystemExit(0)

try:
    payload = json.loads(body)
except Exception:
    print("")
    raise SystemExit(0)

for item in payload.get("uploaded_images", []) or []:
    rid = str(item.get("resource_id", "")).strip()
    if rid:
        print(rid)
        raise SystemExit(0)

recovered = payload.get("recovered", {}) or {}
request = recovered.get("request", {}) or {}
for key in ("resource_id",):
    rid = str(request.get(key, "")).strip()
    if rid:
        print(rid)
        raise SystemExit(0)

for key in ("resource_id_list",):
    values = request.get(key, []) or []
    if isinstance(values, list) and values:
        rid = str(values[0]).strip()
        if rid:
            print(rid)
            raise SystemExit(0)

print("")
PY
}

resolve_resource_id_from_case_log() {
  local stem="$1"
  local submit_id
  submit_id="$(resolve_submit_id_from_case_log "${stem}")" || return 1
  local resource_id
  resource_id="$(resolve_resource_id_from_submit "${submit_id}")"
  if [[ -n "${resource_id}" ]]; then
    printf '%s' "${resource_id}"
    return
  fi
  echo "unable to resolve resource_id from ${stem}; run the corresponding CLI smoke first" >&2
  return 1
}

resolve_default_image_resource_id() {
  local resource_id=""
  if ! is_placeholder_value "${RESOURCE_ID:-}"; then
    printf '%s' "${RESOURCE_ID}"
    return
  fi
  resource_id="$(resolve_resource_id_from_case_log "cli_image2image" || true)"
  if [[ -n "${resource_id}" ]]; then
    printf '%s' "${resource_id}"
    return
  fi
  resource_id="$(resolve_resource_id_from_case_log "cli_image_upscale" || true)"
  if [[ -n "${resource_id}" ]]; then
    printf '%s' "${resource_id}"
    return
  fi
  echo "unable to resolve default image resource id automatically; run cli:image2image or cli:image_upscale first" >&2
  return 1
}

resolve_first_resource_id() {
  local resource_id=""
  if ! is_placeholder_value "${FIRST_RESOURCE_ID:-}"; then
    printf '%s' "${FIRST_RESOURCE_ID}"
    return
  fi
  resource_id="$(resolve_resource_id_from_case_log "cli_image2image" || true)"
  if [[ -n "${resource_id}" ]]; then
    printf '%s' "${resource_id}"
    return
  fi
  resolve_default_image_resource_id
}

resolve_last_resource_id() {
  local resource_id=""
  if ! is_placeholder_value "${LAST_RESOURCE_ID:-}"; then
    printf '%s' "${LAST_RESOURCE_ID}"
    return
  fi
  resource_id="$(resolve_resource_id_from_case_log "cli_image_upscale" || true)"
  if [[ -n "${resource_id}" ]]; then
    printf '%s' "${resource_id}"
    return
  fi
  resolve_default_image_resource_id
}

fetch_query_result_body() {
  local submit_id="$1"
  local cookie cli_version
  cookie="$(resolve_cookie)"
  cli_version="$(resolve_cli_version)"
  [[ -n "${submit_id}" ]] || return 1
  [[ -n "${cookie}" ]] || return 1
  [[ -n "${cli_version}" ]] || return 1
  curl -sS -X POST \
    "${BASE_URL}/mweb/v1/history/query?aid=513695&from=dreamina_cli&cli_version=${cli_version}" \
    -H "Accept: application/json" \
    -H "Appid: 513695" \
    -H "Pf: 7" \
    -H "Cookie: ${cookie}" \
    -H "Content-Type: application/json" \
    -H "X-Tt-Logid: ${LOGID}" \
    -H "X-Use-Ppe: 1" \
    --data '{"submit_ids":["'"${submit_id}"'"]}'
}

resolve_multimodal_resource_triplet_from_submit() {
  local submit_id="$1"
  local default_image_id="${2:-}"
  local default_video_id="${3:-}"
  local default_audio_id="${4:-}"
  local body=""
  [[ -n "${submit_id}" ]] || {
    printf '%s\t%s\t%s' "${default_image_id}" "${default_video_id}" "${default_audio_id}"
    return
  }
  body="$(sqlite3 ~/.dreamina_cli/tasks.db "select result_json from aigc_task where submit_id='${submit_id}' limit 1;" 2>/dev/null || true)"
  RESULT_JSON="${body}" DEFAULT_IMAGE_ID="${default_image_id}" DEFAULT_VIDEO_ID="${default_video_id}" DEFAULT_AUDIO_ID="${default_audio_id}" python3 - <<'PY'
import json
import os

body = os.environ.get("RESULT_JSON", "").strip()
image_id = os.environ.get("DEFAULT_IMAGE_ID", "").strip()
video_id = os.environ.get("DEFAULT_VIDEO_ID", "").strip()
audio_id = os.environ.get("DEFAULT_AUDIO_ID", "").strip()

if not body:
    print("\t".join([image_id, video_id, audio_id]))
    raise SystemExit(0)

try:
    payload = json.loads(body)
except Exception:
    print("\t".join([image_id, video_id, audio_id]))
    raise SystemExit(0)

request = payload.get("request", {}) or {}

def first_from_list(key: str) -> str:
    values = request.get(key, []) or []
    if isinstance(values, list):
        for value in values:
            text = str(value).strip()
            if text:
                return text
    return ""

if not image_id:
    image_id = first_from_list("image_resource_id_list")
if not video_id:
    video_id = first_from_list("video_resource_id_list")
if not audio_id:
    audio_id = first_from_list("audio_resource_id_list")

def first_uploaded(items):
    for item in items or []:
        rid = str((item or {}).get("resource_id", "")).strip()
        if rid:
            return rid
    return ""

if not image_id:
    image_id = first_uploaded(payload.get("uploaded_images"))
if not video_id:
    video_id = first_uploaded(payload.get("uploaded_videos"))
if not audio_id:
    audio_id = first_uploaded(payload.get("uploaded_audios"))

print("\t".join([image_id, video_id, audio_id]))
PY
}

resolve_multimodal_resource_triplet() {
  local submit_id body
  local default_image_id="${1:-}"
  local default_video_id="${2:-}"
  local default_audio_id="${3:-}"

  submit_id="$(resolve_multimodal_submit_id || true)"
  if [[ -z "${submit_id}" ]]; then
    printf '%s\t%s\t%s' "${default_image_id}" "${default_video_id}" "${default_audio_id}"
    return
  fi

  local local_triplet=""
  local_triplet="$(resolve_multimodal_resource_triplet_from_submit "${submit_id}" "${default_image_id}" "${default_video_id}" "${default_audio_id}")"
  if [[ -n "${local_triplet}" ]]; then
    local local_image_id local_video_id local_audio_id
    local_image_id="$(printf '%s' "${local_triplet}" | cut -f1)"
    local_video_id="$(printf '%s' "${local_triplet}" | cut -f2)"
    local_audio_id="$(printf '%s' "${local_triplet}" | cut -f3)"
    if [[ -n "${local_image_id}" || -n "${local_video_id}" || -n "${local_audio_id}" ]]; then
      printf '%s' "${local_triplet}"
      return
    fi
  fi

  body="$(fetch_query_result_body "${submit_id}" || true)"
  QUERY_RESULT_BODY="${body}" DEFAULT_IMAGE_ID="${default_image_id}" DEFAULT_VIDEO_ID="${default_video_id}" DEFAULT_AUDIO_ID="${default_audio_id}" python3 - <<'PY'
import json
import os

body = os.environ.get("QUERY_RESULT_BODY", "").strip()
image_id = os.environ.get("DEFAULT_IMAGE_ID", "").strip()
video_id = os.environ.get("DEFAULT_VIDEO_ID", "").strip()
audio_id = os.environ.get("DEFAULT_AUDIO_ID", "").strip()

if not body:
    print("\t".join([image_id, video_id, audio_id]))
    raise SystemExit(0)

try:
    payload = json.loads(body)
except Exception:
    print("\t".join([image_id, video_id, audio_id]))
    raise SystemExit(0)

if str(payload.get("ret", "")).strip() not in {"0", ""}:
    print("\t".join([image_id, video_id, audio_id]))
    raise SystemExit(0)

data = payload.get("data", {}) or {}
for task in data.values():
    resources = task.get("resources", []) or []
    for item in resources:
      item_type = str(item.get("type", "")).strip().lower()
      if item_type == "image" and not image_id:
          image_info = item.get("image_info", {}) or {}
          image_id = (
              str(item.get("key", "")).strip()
              or str(image_info.get("image_uri", "")).strip()
              or str(image_info.get("uri", "")).strip()
          )
      elif item_type == "video" and not video_id:
          video_info = item.get("video_info", {}) or {}
          origin = video_info.get("origin_video", {}) or {}
          video_id = (
              str(video_info.get("video_id", "")).strip()
              or str(origin.get("vid", "")).strip()
              or str(item.get("key", "")).strip()
          )
      elif item_type == "audio" and not audio_id:
          audio_info = item.get("audio_info", {}) or {}
          video_info = item.get("video_info", {}) or {}
          origin = video_info.get("origin_video", {}) or {}
          audio_id = (
              str(audio_info.get("vid", "")).strip()
              or str(video_info.get("video_id", "")).strip()
              or str(origin.get("vid", "")).strip()
              or str(item.get("key", "")).strip()
          )
    break

print("\t".join([image_id, video_id, audio_id]))
PY
}

resolve_image_path() {
  if ! is_placeholder_value "${IMAGE_PATH:-}"; then
    printf '%s' "${IMAGE_PATH}"
    return
  fi
  if [[ -f "${ROOT_DIR}/testdata/smoke/image-1.png" ]]; then
    printf '%s' "${ROOT_DIR}/testdata/smoke/image-1.png"
    return
  fi
  echo "unable to resolve IMAGE_PATH automatically; set IMAGE_PATH to a local image file" >&2
  return 1
}

resolve_ffmpeg_bin() {
  if command -v ffmpeg >/dev/null 2>&1; then
    command -v ffmpeg
    return
  fi
  if [[ -x /opt/homebrew/bin/ffmpeg ]]; then
    echo /opt/homebrew/bin/ffmpeg
    return
  fi
  echo "ffmpeg not found; install ffmpeg or add it to PATH" >&2
  return 1
}

ensure_smoke_video_sample() {
  local ffmpeg_bin image_path sample_dir sample_path
  ffmpeg_bin="$(resolve_ffmpeg_bin)"
  image_path="$(resolve_image_path)"
  sample_dir="${ROOT_DIR}/testdata/smoke"
  sample_path="${sample_dir}/ref5.mp4"
  mkdir -p "${sample_dir}"
  if [[ -f "${sample_path}" ]]; then
    printf '%s' "${sample_path}"
    return
  fi
  "${ffmpeg_bin}" -y \
    -loop 1 \
    -i "${image_path}" \
    -f lavfi \
    -i anullsrc=r=44100:cl=stereo \
    -t 5 \
    -c:v libx264 \
    -pix_fmt yuv420p \
    -vf scale=1280:720 \
    -c:a aac \
    "${sample_path}" >/dev/null 2>&1
  printf '%s' "${sample_path}"
}

ensure_smoke_audio_sample() {
  local ffmpeg_bin sample_dir sample_path
  ffmpeg_bin="$(resolve_ffmpeg_bin)"
  sample_dir="${ROOT_DIR}/testdata/smoke"
  sample_path="${sample_dir}/music5.mp3"
  mkdir -p "${sample_dir}"
  if [[ -f "${sample_path}" ]]; then
    printf '%s' "${sample_path}"
    return
  fi
  "${ffmpeg_bin}" -y \
    -f lavfi \
    -i sine=frequency=440:sample_rate=44100 \
    -t 5 \
    -q:a 2 \
    "${sample_path}" >/dev/null 2>&1
  printf '%s' "${sample_path}"
}

build_commerce_logid() {
  local path="$1"
  local ts="$2"
  python3 - "${path}" "${ts}" <<'PY'
import hashlib
import sys

path = sys.argv[1].strip()
ts = sys.argv[2].strip()
print(hashlib.md5(f"{path}|{ts}".encode("utf-8")).hexdigest())
PY
}

build_commerce_sign() {
  local path="$1"
  local pf="$2"
  local app_version="$3"
  local ts="$4"
  local tdid="${5:-}"
  python3 - "${path}" "${pf}" "${app_version}" "${ts}" "${tdid}" <<'PY'
import hashlib
import sys

path = sys.argv[1].strip()
pf = sys.argv[2].strip() or "7"
app_version = sys.argv[3].strip() or "8.4.0"
ts = sys.argv[4].strip()
tdid = sys.argv[5].strip()

if len(path) > 7:
    path = path[-7:]
raw = f"9e2c|{path}|{pf}|{app_version}|{ts}|{tdid}|11ac"
print(hashlib.md5(raw.encode("utf-8")).hexdigest())
PY
}

print_plan() {
  cat <<'EOF'
Recommended order:
  1. dreamina login
  2. dreamina validate-auth-token
  3. dreamina user_credit
  4. dreamina text2video --prompt "未来城市清晨航拍" --poll 10
  5. dreamina query_result --submit_id "<submit_id>"
  6. dreamina image2image --image ./input.png --prompt "改成夜景霓虹风格" --poll 10
  7. dreamina multimodal2video --image ./cover.png --video ./bg.mp4 --audio ./music.mp3 --prompt "生成统一短片" --poll 10

Curl replay order:
  1. curl:user_credit
  2. curl:text2video
  3. curl:query_result
  4. curl:text2image
  5. curl:image2image

Image upload order:
  1. cli:image_upscale
  2. cli:image_upscale_debug
  3. cli:image2image
  4. cli:query_image_upscale
  5. cli:download_image_upscale
EOF
}

ensure_run_dir() {
  mkdir -p "${RUN_DIR}"
}

init_results_md() {
  ensure_run_dir
  if [[ -f "${RESULTS_MD}" ]]; then
    return
  fi
  cat > "${RESULTS_MD}" <<EOF
# Dreamina Smoke Results

- run_id: ${RUN_ID}
- started_at: $(date '+%Y-%m-%d %H:%M:%S %z')
- root_dir: ${ROOT_DIR}

| case | status | exit_code | stdout | stderr |
| --- | --- | --- | --- | --- |
EOF
}

slugify() {
  local value="$1"
  value="${value//:/_}"
  value="${value//\//_}"
  value="${value// /_}"
  echo "${value}"
}

append_results_row() {
  local case_name="$1"
  local status="$2"
  local exit_code="$3"
  local stdout_file="$4"
  local stderr_file="$5"
  init_results_md
  printf '| `%s` | `%s` | `%s` | `%s` | `%s` |\n' \
    "${case_name}" \
    "${status}" \
    "${exit_code}" \
    "$(basename "${stdout_file}")" \
    "$(basename "${stderr_file}")" >> "${RESULTS_MD}"
}

append_case_detail() {
  local case_name="$1"
  local status="$2"
  local exit_code="$3"
  local started_at="$4"
  local ended_at="$5"
  local stdout_file="$6"
  local stderr_file="$7"

  {
    echo
    echo "## ${case_name}"
    echo
    echo "- status: ${status}"
    echo "- exit_code: ${exit_code}"
    echo "- started_at: ${started_at}"
    echo "- ended_at: ${ended_at}"
    echo "- stdout: $(basename "${stdout_file}")"
    echo "- stderr: $(basename "${stderr_file}")"
    echo
    echo "### stdout preview"
    echo
    echo '```text'
    sed -n '1,40p' "${stdout_file}"
    echo '```'
    echo
    echo "### stderr preview"
    echo
    echo '```text'
    sed -n '1,40p' "${stderr_file}"
    echo '```'
    append_case_annotations "${case_name}" "${stdout_file}" "${stderr_file}"
  } >> "${RESULTS_MD}"
}

append_case_annotations() {
  local case_name="$1"
  local stdout_file="$2"
  local stderr_file="$3"

  case "${case_name}" in
    curl:multimodal2video)
      python3 - "${stderr_file}" <<'PY'
import re
import sys
from pathlib import Path

text = Path(sys.argv[1]).read_text(encoding="utf-8", errors="replace")
pattern = re.compile(
    r"\[smoke\] curl:multimodal2video image_resource_id=(?P<image>\S+) "
    r"video_resource_id=(?P<video>\S+) audio_resource_id=(?P<audio>\S+)"
)
match = pattern.search(text)
if not match:
    raise SystemExit(0)

image = match.group("image")
video = match.group("video")
audio = match.group("audio")
payload_mode = "full"
if video == "<empty>" and audio == "<empty>":
    payload_mode = "image_only"
elif video == "<empty>" or audio == "<empty>":
    payload_mode = "partial"

print()
print("### detected payload resources")
print()
print(f"- payload_mode: {payload_mode}")
print(f"- image_resource_id: {image}")
print(f"- video_resource_id: {video}")
print(f"- audio_resource_id: {audio}")
PY
      ;;
  esac
}

validate_case_output() {
  local case_name="$1"
  local stdout_file="$2"

  case "${case_name}" in
    curl:*)
      python3 - "${stdout_file}" <<'PY'
import json
import sys
from pathlib import Path

path = Path(sys.argv[1])
body = path.read_text(encoding="utf-8").strip()
if not body:
    print("empty stdout", file=sys.stderr)
    raise SystemExit(1)

try:
    payload = json.loads(body)
except Exception as exc:
    print(f"invalid json: {exc}", file=sys.stderr)
    raise SystemExit(1)

def first_key(d, keys):
    if not isinstance(d, dict):
        return None
    for key in keys:
        if key in d:
            return d[key]
    return None

ret = first_key(payload, ["ret", "Ret", "code", "Code"])
if ret is None:
    raise SystemExit(0)

ret_text = str(ret).strip()
if ret_text not in {"0", ""}:
    print(f"business ret/code not success: {ret_text}", file=sys.stderr)
    raise SystemExit(1)
PY
      ;;
  esac
}

run_case() {
  local case_name="$1"
  shift

  ensure_run_dir
  init_results_md

  local slug
  slug="$(slugify "${case_name}")"
  local stdout_file="${RUN_DIR}/${slug}.stdout.log"
  local stderr_file="${RUN_DIR}/${slug}.stderr.log"
  local started_at ended_at exit_code status
  started_at="$(date '+%Y-%m-%d %H:%M:%S %z')"

  set +e
  "$@" >"${stdout_file}" 2>"${stderr_file}"
  exit_code=$?
  set -e

  if [[ "${exit_code}" -eq 0 ]]; then
    set +e
    validate_case_output "${case_name}" "${stdout_file}" >>"${stdout_file}" 2>>"${stderr_file}"
    exit_code=$?
    set -e
  fi

  ended_at="$(date '+%Y-%m-%d %H:%M:%S %z')"
  status="passed"
  if [[ "${exit_code}" -ne 0 ]]; then
    status="failed"
  fi

  append_results_row "${case_name}" "${status}" "${exit_code}" "${stdout_file}" "${stderr_file}"
  append_case_detail "${case_name}" "${status}" "${exit_code}" "${started_at}" "${ended_at}" "${stdout_file}" "${stderr_file}"

  echo "case=${case_name} status=${status} exit_code=${exit_code}" >&2
  echo "results_md=${RESULTS_MD}" >&2
  echo "stdout_log=${stdout_file}" >&2
  echo "stderr_log=${stderr_file}" >&2

  return "${exit_code}"
}

dispatch_case() {
  local cmd="$1"
  case "${cmd}" in
    cli:user_credit) run_case "${cmd}" run_cli_user_credit ;;
    cli:text2video) run_case "${cmd}" run_cli_text2video ;;
    cli:image2image) run_case "${cmd}" run_cli_image2image ;;
    cli:image_upscale) run_case "${cmd}" run_cli_image_upscale ;;
    cli:image_upscale_debug) run_case "${cmd}" run_cli_image_upscale_debug ;;
    cli:multimodal2video) run_case "${cmd}" run_cli_multimodal2video ;;
    cli:query_image2image) run_case "${cmd}" run_cli_query_image2image ;;
    cli:query_image_upscale) run_case "${cmd}" run_cli_query_image_upscale ;;
    cli:download_image_upscale) run_case "${cmd}" run_cli_download_image_upscale ;;
    curl:user_credit) run_case "${cmd}" run_curl_user_credit ;;
    curl:query_result) run_case "${cmd}" run_curl_query_result ;;
    curl:text2image) run_case "${cmd}" run_curl_text2image ;;
    curl:text2video) run_case "${cmd}" run_curl_text2video ;;
    curl:image2image) run_case "${cmd}" run_curl_image2image ;;
    curl:image_upscale) run_case "${cmd}" run_curl_image_upscale ;;
    curl:image2video) run_case "${cmd}" run_curl_image2video ;;
    curl:frames2video) run_case "${cmd}" run_curl_frames2video ;;
    curl:multiframe2video) run_case "${cmd}" run_curl_multiframe2video ;;
    curl:ref2video) run_case "${cmd}" run_curl_ref2video ;;
    curl:multimodal2video) run_case "${cmd}" run_curl_multimodal2video ;;
    *)
      echo "unknown case: ${cmd}" >&2
      return 1
      ;;
  esac
}

run_batch() {
  local batch_name="$1"
  shift

  ensure_run_dir
  init_results_md

  local case_name
  local failed=0

  echo "running batch=${batch_name} run_dir=${RUN_DIR}" >&2
  for case_name in "$@"; do
    if ! dispatch_case "${case_name}"; then
      failed=1
    fi
  done

  echo "batch=${batch_name} completed failed=${failed}" >&2
  echo "results_md=${RESULTS_MD}" >&2
  return "${failed}"
}

run_cli_user_credit() {
  local bin
  bin="$(resolve_dreamina_bin)"
  (cd "${ROOT_DIR}" && "${bin}" user_credit)
}

run_cli_text2video() {
  local bin
  bin="$(resolve_dreamina_bin)"
  (cd "${ROOT_DIR}" && "${bin}" text2video --prompt "未来城市清晨航拍" --model_version "${TEXT2VIDEO_MODEL_VERSION}" --poll 10)
}

run_cli_image2image() {
  local bin image_path
  bin="$(resolve_dreamina_bin)"
  image_path="$(resolve_image_path)"
  (cd "${ROOT_DIR}" && "${bin}" image2image --image "${image_path}" --prompt "改成夜景霓虹风格" --poll 10)
}

run_cli_image_upscale() {
  local bin image_path
  bin="$(resolve_dreamina_bin)"
  image_path="$(resolve_image_path)"
  (cd "${ROOT_DIR}" && "${bin}" image_upscale --image "${image_path}" --resolution_type 4k)
}

run_cli_image_upscale_debug() {
  local bin image_path
  bin="$(resolve_dreamina_bin)"
  image_path="$(resolve_image_path)"
  (cd "${ROOT_DIR}" && DREAMINA_DEBUG=1 "${bin}" image_upscale --image "${image_path}" --resolution_type 4k --poll 0)
}

run_cli_multimodal2video() {
  local bin image_path video_path audio_path
  bin="$(resolve_dreamina_bin)"
  image_path="$(resolve_image_path)"
  video_path="$(ensure_smoke_video_sample)"
  audio_path="$(ensure_smoke_audio_sample)"
  (cd "${ROOT_DIR}" && "${bin}" multimodal2video --image "${image_path}" --video "${video_path}" --audio "${audio_path}" --prompt "生成统一短片" --model_version "${MULTIMODAL_MODEL_VERSION}" --duration 5 --poll 10)
}

run_cli_query_image2image() {
  local bin submit_id
  bin="$(resolve_dreamina_bin)"
  submit_id="$(resolve_submit_id_from_case_log "cli_image2image")"
  (cd "${ROOT_DIR}" && "${bin}" query_result --submit_id "${submit_id}")
}

run_cli_query_image_upscale() {
  local bin submit_id
  bin="$(resolve_dreamina_bin)"
  submit_id="$(resolve_submit_id_from_case_log "cli_image_upscale")"
  (cd "${ROOT_DIR}" && "${bin}" query_result --submit_id "${submit_id}")
}

run_cli_download_image_upscale() {
  local bin submit_id download_dir
  bin="$(resolve_dreamina_bin)"
  submit_id="$(resolve_submit_id_from_case_log "cli_image_upscale")"
  download_dir="${ROOT_DIR}/tmp_smoke_downloads"
  rm -rf "${download_dir}"
  mkdir -p "${download_dir}"
  (cd "${ROOT_DIR}" && "${bin}" query_result --submit_id "${submit_id}" --download_dir "${download_dir}")
}

run_curl_user_credit() {
  local cookie path ts logid sign
  local pf appvr app_sdk_version accept_language sec_ch_ua sec_ch_ua_platform
  local user_agent referer lan priority
  local -a args

  path="/commerce/v1/benefits/user_credit"
  cookie="$(resolve_cookie)"
  [[ -n "${cookie}" ]] || { echo "missing required value: COOKIE" >&2; return 1; }

  pf="7"
  appvr="8.4.0"
  app_sdk_version="48.0.0"
  accept_language="en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7"
  sec_ch_ua='"Chromium";v="146", "Not-A.Brand";v="24", "Google Chrome";v="146"'
  sec_ch_ua_platform='"macOS"'
  ts="$(date +%s)"
  logid="$(build_commerce_logid "${path}" "${ts}")"
  sign="$(build_commerce_sign "${path}" "${pf}" "${appvr}" "${ts}" "")"

  user_agent="$(resolve_session_field header "User-Agent")"
  referer="$(resolve_session_field header "Referer")"
  lan="$(resolve_session_field header "Lan")"
  priority="$(resolve_session_field header "Priority")"

  args=(
    curl -X POST
    "${BASE_URL}/commerce/v1/benefits/user_credit?aid=513695"
    -H "Accept: application/json"
    -H "Content-Type: application/json"
    -H "Cookie: ${cookie}"
    -H "Appid: 513695"
    -H "Pf: ${pf}"
    -H "Appvr: ${appvr}"
    -H "App-Sdk-Version: ${app_sdk_version}"
    -H "Device-Time: ${ts}"
    -H "Sign: ${sign}"
    -H "Sign-Ver: 1"
    -H "X-Client-Scheme: https"
    -H "X-Tt-Logid: ${logid}"
    -H "X-Use-Ppe: 1"
    -H "Accept-Language: ${accept_language}"
    -H "Sec-Fetch-Mode: cors"
    -H "Sec-Fetch-Site: same-origin"
    -H "Sec-CH-UA-Mobile: ?0"
    -H "Sec-CH-UA-Platform: ${sec_ch_ua_platform}"
    -H "Sec-CH-UA: ${sec_ch_ua}"
  )
  [[ -n "${user_agent}" ]] && args+=(-H "User-Agent: ${user_agent}")
  [[ -n "${referer}" ]] && args+=(-H "Referer: ${referer}")
  [[ -n "${lan}" ]] && args+=(-H "Lan: ${lan}")
  [[ -n "${priority}" ]] && args+=(-H "Priority: ${priority}")
  args+=(--data '{}')
  "${args[@]}"
}

run_curl_query_result() {
  local cookie cli_version submit_id
  cookie="$(resolve_cookie)"
  cli_version="$(resolve_cli_version)"
  submit_id="$(resolve_submit_id)"
  [[ -n "${cookie}" ]] || { echo "missing required value: COOKIE" >&2; return 1; }
  [[ -n "${cli_version}" ]] || { echo "missing required value: CLI_VERSION" >&2; return 1; }
  [[ -n "${submit_id}" ]] || { echo "missing required value: SUBMIT_ID" >&2; return 1; }
  curl -X POST \
    "${BASE_URL}/mweb/v1/get_history_by_ids?aid=513695&from=dreamina_cli&cli_version=${cli_version}" \
    -H "Accept: application/json" \
    -H "Appid: 513695" \
    -H "Pf: 7" \
    -H "Cookie: ${cookie}" \
    -H "Content-Type: application/json" \
    -H "X-Tt-Logid: ${LOGID}" \
    -H "X-Use-Ppe: 1" \
    --data '{
      "submit_ids": ["'"${submit_id}"'"],
      "history_ids": [],
      "need_batch": false
    }'
}

run_curl_text2image() {
  local cookie cli_version
  cookie="$(resolve_cookie)"
  cli_version="$(resolve_cli_version)"
  [[ -n "${cookie}" ]] || { echo "missing required value: COOKIE" >&2; return 1; }
  [[ -n "${cli_version}" ]] || { echo "missing required value: CLI_VERSION" >&2; return 1; }
  curl -X POST \
    "${BASE_URL}/dreamina/cli/v1/image_generate?aid=513695&from=dreamina_cli&cli_version=${cli_version}" \
    -H "Accept: application/json" \
    -H "Appid: 513695" \
    -H "Pf: 7" \
    -H "Cookie: ${cookie}" \
    -H "Content-Type: application/json" \
    -H "X-Tt-Logid: ${LOGID}" \
    -H "X-Use-Ppe: 1" \
    --data '{
      "agent_scene": "workbench",
      "creation_agent_version": "3.0.0",
      "generate_type": "text2imageByConfig",
      "prompt": "一只白色机械猫，电影感光影",
      "ratio": "16:9",
      "submit_id": "__AUTO_GENERATED__",
      "subject_id": "__AUTO_GENERATED__"
    }'
}

run_curl_text2video() {
  local cookie cli_version
  cookie="$(resolve_cookie)"
  cli_version="$(resolve_cli_version)"
  [[ -n "${cookie}" ]] || { echo "missing required value: COOKIE" >&2; return 1; }
  [[ -n "${cli_version}" ]] || { echo "missing required value: CLI_VERSION" >&2; return 1; }
  curl -X POST \
    "${BASE_URL}/dreamina/cli/v1/video_generate?aid=513695&from=dreamina_cli&cli_version=${cli_version}" \
    -H "Accept: application/json" \
    -H "Appid: 513695" \
    -H "Pf: 7" \
    -H "Cookie: ${cookie}" \
    -H "Content-Type: application/json" \
    -H "X-Tt-Logid: ${LOGID}" \
    -H "X-Use-Ppe: 1" \
    --data '{
      "generate_type": "text2VideoByConfig",
      "agent_scene": "workbench",
      "prompt": "未来城市清晨航拍",
      "ratio": "16:9",
      "duration": 5,
      "creation_agent_version": "3.0.0",
      "model_key": "'"${TEXT2VIDEO_MODEL_VERSION}"'",
      "submit_id": "__AUTO_GENERATED__"
    }'
}

run_curl_image2image() {
  local cookie cli_version resource_id
  cookie="$(resolve_cookie)"
  cli_version="$(resolve_cli_version)"
  [[ -n "${cookie}" ]] || { echo "missing required value: COOKIE" >&2; return 1; }
  [[ -n "${cli_version}" ]] || { echo "missing required value: CLI_VERSION" >&2; return 1; }
  if is_placeholder_value "${RESOURCE_ID:-}"; then
    resource_id="$(resolve_resource_id_from_case_log "cli_image2image")"
  else
    resource_id="${RESOURCE_ID}"
  fi
  [[ -n "${resource_id}" ]] || { echo "missing required value: RESOURCE_ID" >&2; return 1; }
  curl -X POST \
    "${BASE_URL}/dreamina/cli/v1/image_generate?aid=513695&from=dreamina_cli&cli_version=${cli_version}" \
    -H "Accept: application/json" \
    -H "Appid: 513695" \
    -H "Pf: 7" \
    -H "Cookie: ${cookie}" \
    -H "Content-Type: application/json" \
    -H "X-Tt-Logid: ${LOGID}" \
    -H "X-Use-Ppe: 1" \
    --data '{
      "agent_scene": "workbench",
      "creation_agent_version": "3.0.0",
      "generate_type": "editImageByConfig",
      "prompt": "把画面改成夜景霓虹风格",
      "ratio": "16:9",
      "resource_id_list": ["'"${resource_id}"'"],
      "submit_id": "__AUTO_GENERATED__",
      "subject_id": "__AUTO_GENERATED__"
    }'
}

run_curl_image_upscale() {
  local cookie cli_version resource_id resolution_type
  cookie="$(resolve_cookie)"
  cli_version="$(resolve_cli_version)"
  [[ -n "${cookie}" ]] || { echo "missing required value: COOKIE" >&2; return 1; }
  [[ -n "${cli_version}" ]] || { echo "missing required value: CLI_VERSION" >&2; return 1; }
  if is_placeholder_value "${RESOURCE_ID:-}"; then
    resource_id="$(resolve_resource_id_from_case_log "cli_image_upscale")"
  else
    resource_id="${RESOURCE_ID}"
  fi
  [[ -n "${resource_id}" ]] || { echo "missing required value: RESOURCE_ID" >&2; return 1; }
  resolution_type="${UPSCALE_RESOLUTION:-4k}"
  curl -X POST \
    "${BASE_URL}/dreamina/cli/v1/image_generate?aid=513695&from=dreamina_cli&cli_version=${cli_version}" \
    -H "Accept: application/json" \
    -H "Appid: 513695" \
    -H "Pf: 7" \
    -H "Cookie: ${cookie}" \
    -H "Content-Type: application/json" \
    -H "X-Tt-Logid: ${LOGID}" \
    -H "X-Use-Ppe: 1" \
    --data '{
      "agent_scene": "workbench",
      "creation_agent_version": "3.0.0",
      "generate_type": "imageSuperResolution",
      "resource_id": "'"${resource_id}"'",
      "resolution_type": "'"${resolution_type}"'",
      "submit_id": "__AUTO_GENERATED__",
      "subject_id": "__AUTO_GENERATED__"
    }'
}

run_curl_image2video() {
  local cookie cli_version
  local resource_id
  cookie="$(resolve_cookie)"
  cli_version="$(resolve_cli_version)"
  [[ -n "${cookie}" ]] || { echo "missing required value: COOKIE" >&2; return 1; }
  [[ -n "${cli_version}" ]] || { echo "missing required value: CLI_VERSION" >&2; return 1; }
  resource_id="$(resolve_default_image_resource_id)"
  [[ -n "${resource_id}" ]] || { echo "missing required value: RESOURCE_ID" >&2; return 1; }
  curl -X POST \
    "${BASE_URL}/dreamina/cli/v1/video_generate?aid=513695&from=dreamina_cli&cli_version=${cli_version}" \
    -H "Accept: application/json" \
    -H "Appid: 513695" \
    -H "Pf: 7" \
    -H "Cookie: ${cookie}" \
    -H "Content-Type: application/json" \
    -H "X-Tt-Logid: ${LOGID}" \
    -H "X-Use-Ppe: 1" \
    --data '{
      "generate_type": "image2video",
      "agent_scene": "workbench",
      "creation_agent_version": "3.0.0",
      "first_frame_resource_id": "'"${resource_id}"'",
      "prompt": "镜头缓慢推进",
      "duration": 5,
      "submit_id": "__AUTO_GENERATED__"
    }'
}

run_curl_frames2video() {
  local cookie cli_version
  local first_resource_id last_resource_id
  cookie="$(resolve_cookie)"
  cli_version="$(resolve_cli_version)"
  [[ -n "${cookie}" ]] || { echo "missing required value: COOKIE" >&2; return 1; }
  [[ -n "${cli_version}" ]] || { echo "missing required value: CLI_VERSION" >&2; return 1; }
  first_resource_id="$(resolve_first_resource_id)"
  last_resource_id="$(resolve_last_resource_id)"
  [[ -n "${first_resource_id}" ]] || { echo "missing required value: FIRST_RESOURCE_ID" >&2; return 1; }
  [[ -n "${last_resource_id}" ]] || { echo "missing required value: LAST_RESOURCE_ID" >&2; return 1; }
  curl -X POST \
    "${BASE_URL}/dreamina/cli/v1/video_generate?aid=513695&from=dreamina_cli&cli_version=${cli_version}" \
    -H "Accept: application/json" \
    -H "Appid: 513695" \
    -H "Pf: 7" \
    -H "Cookie: ${cookie}" \
    -H "Content-Type: application/json" \
    -H "X-Tt-Logid: ${LOGID}" \
    -H "X-Use-Ppe: 1" \
    --data '{
      "generate_type": "startEndFrameVideoByConfig",
      "agent_scene": "workbench",
      "creation_agent_version": "3.0.0",
      "first_frame_resource_id": "'"${first_resource_id}"'",
      "last_frame_resource_id": "'"${last_resource_id}"'",
      "prompt": "从白天平稳过渡到夜晚霓虹",
      "duration": 5,
      "submit_id": "__AUTO_GENERATED__"
    }'
}

run_curl_multiframe2video() {
  local cookie cli_version
  local first_resource_id last_resource_id
  cookie="$(resolve_cookie)"
  cli_version="$(resolve_cli_version)"
  [[ -n "${cookie}" ]] || { echo "missing required value: COOKIE" >&2; return 1; }
  [[ -n "${cli_version}" ]] || { echo "missing required value: CLI_VERSION" >&2; return 1; }
  first_resource_id="$(resolve_first_resource_id)"
  last_resource_id="$(resolve_last_resource_id)"
  [[ -n "${first_resource_id}" ]] || { echo "missing required value: FIRST_RESOURCE_ID" >&2; return 1; }
  [[ -n "${last_resource_id}" ]] || { echo "missing required value: LAST_RESOURCE_ID" >&2; return 1; }
  curl -X POST \
    "${BASE_URL}/dreamina/cli/v1/video_generate?aid=513695&from=dreamina_cli&cli_version=${cli_version}" \
    -H "Accept: application/json" \
    -H "Appid: 513695" \
    -H "Pf: 7" \
    -H "Cookie: ${cookie}" \
    -H "Content-Type: application/json" \
    -H "X-Tt-Logid: ${LOGID}" \
    -H "X-Use-Ppe: 1" \
    --data '{
      "generate_type": "multiFrame2video",
      "agent_scene": "workbench",
      "creation_agent_version": "3.0.0",
      "submit_id": "__AUTO_GENERATED__",
      "media_resource_id_list": ["'"${first_resource_id}"'", "'"${last_resource_id}"'", "'"${first_resource_id}"'"],
      "media_type_list": ["图片", "图片", "图片"],
      "prompt_list": ["从图1过渡到图2", "从图2过渡到图3"],
      "duration_list": [2.0, 2.0]
    }'
}

run_curl_ref2video() {
  run_curl_multiframe2video
}

run_curl_multimodal2video() {
  local cookie cli_version
  local image_resource_id video_resource_id audio_resource_id resource_triplet
  cookie="$(resolve_cookie)"
  cli_version="$(resolve_cli_version)"
  [[ -n "${cookie}" ]] || { echo "missing required value: COOKIE" >&2; return 1; }
  [[ -n "${cli_version}" ]] || { echo "missing required value: CLI_VERSION" >&2; return 1; }
  image_resource_id="$(resolve_default_image_resource_id)"
  [[ -n "${image_resource_id}" ]] || { echo "missing required value: RESOURCE_ID" >&2; return 1; }
  if ! is_placeholder_value "${VIDEO_RESOURCE_ID:-}"; then
    video_resource_id="${VIDEO_RESOURCE_ID}"
  else
    video_resource_id=""
  fi
  if ! is_placeholder_value "${AUDIO_RESOURCE_ID:-}"; then
    audio_resource_id="${AUDIO_RESOURCE_ID}"
  else
    audio_resource_id=""
  fi
  resource_triplet="$(resolve_multimodal_resource_triplet "${image_resource_id}" "${video_resource_id}" "${audio_resource_id}")"
  image_resource_id="$(printf '%s' "${resource_triplet}" | cut -f1)"
  video_resource_id="$(printf '%s' "${resource_triplet}" | cut -f2)"
  audio_resource_id="$(printf '%s' "${resource_triplet}" | cut -f3)"
  echo "[smoke] curl:multimodal2video image_resource_id=${image_resource_id} video_resource_id=${video_resource_id:-<empty>} audio_resource_id=${audio_resource_id:-<empty>}" >&2
  curl -X POST \
    "${BASE_URL}/dreamina/cli/v1/video_generate?aid=513695&from=dreamina_cli&cli_version=${cli_version}" \
    -H "Accept: application/json" \
    -H "Appid: 513695" \
    -H "Pf: 7" \
    -H "Cookie: ${cookie}" \
    -H "Content-Type: application/json" \
    -H "X-Tt-Logid: ${LOGID}" \
    -H "X-Use-Ppe: 1" \
    --data '{
      "generate_type": "multiModal2VideoByConfig",
      "agent_scene": "workbench",
      "creation_agent_version": "3.0.0",
      "submit_id": "__AUTO_GENERATED__",
      "prompt": "以图像为主视觉生成统一短片",
      "ratio": "16:9",
      "duration": 5,
      "model_key": "'"${MULTIMODAL_MODEL_VERSION}"'",
      "image_resource_id_list": ["'"${image_resource_id}"'"],
      "video_resource_id_list": '"$(if [[ -n "${video_resource_id}" ]]; then printf '["%s"]' "${video_resource_id}"; else printf '[]'; fi)"',
      "audio_resource_id_list": '"$(if [[ -n "${audio_resource_id}" ]]; then printf '["%s"]' "${audio_resource_id}"; else printf '[]'; fi)"'
    }'
}

main() {
  local cmd="${1:-plan}"
  case "${cmd}" in
    plan) print_plan ;;
    batch:core) run_batch "${cmd}" cli:user_credit cli:text2video ;;
    batch:image-core) run_batch "${cmd}" cli:image_upscale cli:image2image ;;
    batch:image-upload-core) run_batch "${cmd}" cli:image_upscale_debug ;;
    batch:video-upload-core) run_batch "${cmd}" cli:multimodal2video ;;
    batch:image-result-core) run_batch "${cmd}" cli:query_image_upscale cli:download_image_upscale cli:query_image2image ;;
    batch:curl-core) run_batch "${cmd}" curl:user_credit curl:text2image curl:text2video curl:query_result curl:multimodal2video ;;
    batch:all) run_batch "${cmd}" \
      cli:user_credit \
      cli:text2video \
      cli:image_upscale \
      cli:image_upscale_debug \
      cli:image2image \
      cli:multimodal2video \
      cli:query_image_upscale \
      cli:download_image_upscale \
      cli:query_image2image \
      curl:user_credit \
      curl:query_result \
      curl:text2image \
      curl:text2video \
      curl:image2image \
      curl:image_upscale \
      curl:image2video \
      curl:frames2video \
      curl:multiframe2video \
      curl:ref2video \
      curl:multimodal2video ;;
    cli:*|curl:*) dispatch_case "${cmd}" ;;
    -h|--help|help) usage ;;
    *) usage; exit 1 ;;
  esac
}

main "$@"
