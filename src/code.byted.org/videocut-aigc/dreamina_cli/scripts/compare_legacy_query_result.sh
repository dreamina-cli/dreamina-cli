#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

OLD_BIN_DEFAULT="${HOME}/.local/bin/dreamina"
NEW_BIN_DEFAULT=""
if [[ -x "${ROOT_DIR}/dist/current/dreamina" ]]; then
  NEW_BIN_DEFAULT="${ROOT_DIR}/dist/current/dreamina"
fi
if command -v go >/dev/null 2>&1; then
  GOOS="$(go env GOOS 2>/dev/null || true)"
  GOARCH="$(go env GOARCH 2>/dev/null || true)"
  if [[ -z "${NEW_BIN_DEFAULT}" && -n "${GOOS}" && -n "${GOARCH}" ]]; then
    NEW_BIN_DEFAULT="${ROOT_DIR}/dist/${GOOS}-${GOARCH}/dreamina"
  fi
fi
if [[ -z "${NEW_BIN_DEFAULT}" ]]; then
  NEW_BIN_DEFAULT="${ROOT_DIR}/dist/darwin-amd64/dreamina"
fi

CREDENTIAL_DEFAULT="${HOME}/.dreamina_cli/credential.json"
if [[ ! -f "${CREDENTIAL_DEFAULT}" && -f "${HOME}/.dreamina_cli/b.json" ]]; then
  CREDENTIAL_DEFAULT="${HOME}/.dreamina_cli/b.json"
fi
TASK_DB_DEFAULT="${HOME}/.dreamina_cli/tasks.db"

OLD_BIN="${OLD_BIN_DEFAULT}"
NEW_BIN="${NEW_BIN_DEFAULT}"
CREDENTIAL_PATH="${CREDENTIAL_DEFAULT}"
TASK_DB_PATH="${TASK_DB_DEFAULT}"
KEEP_TMP="0"
JSON_STDOUT="0"
JSON_OUTPUT=""
SUBMIT_IDS=()
SUBMIT_ID_FILE=""

usage() {
  cat <<EOF
用法：
  bash scripts/compare_legacy_query_result.sh --submit_id <id> [--submit_id <id> ...]
  bash scripts/compare_legacy_query_result.sh --submit_id_file testdata/legacy/query_result_submit_ids.txt

可选参数：
  --old-bin <path>         旧二进制路径，默认：${OLD_BIN_DEFAULT}
  --new-bin <path>         新二进制路径，默认：${NEW_BIN_DEFAULT}
  --credential <path>      凭证文件，默认：${CREDENTIAL_DEFAULT}
  --tasks-db <path>        任务库文件，默认：${TASK_DB_DEFAULT}
  --submit_id_file <path>  从文件读取 submit_id，支持空行和 # 注释
  --keep-tmp               保留临时 HOME，便于排查
  --json                   仅输出 JSON 到标准输出
  --json-output <path>     额外写出 JSON 文件，便于 CI 或脚本消费
  -h, --help               显示帮助

说明：
  1. 脚本会为旧/新二进制分别创建临时 HOME，复制 credential.json 与 tasks.db*。
  2. 同时比较完整 URL 与归一化资源路径。
  3. 如果只差域名、签名串、查询参数或分发层滚动值，会标记为 signed_url_only。
  4. 所有样本跑完后会输出一段 summary 汇总。
  5. 如果传入 --json，则标准输出只保留最终 JSON。
EOF
}

require_file() {
  local path="$1"
  local label="$2"
  if [[ ! -e "${path}" ]]; then
    echo "${label} 不存在：${path}" >&2
    exit 1
  fi
}

load_submit_ids_from_file() {
  local path="$1"
  while IFS= read -r line || [[ -n "${line}" ]]; do
    line="${line%%#*}"
    line="$(printf '%s' "${line}" | tr -d '\r' | xargs)"
    [[ -n "${line}" ]] || continue
    SUBMIT_IDS+=("${line}")
  done < "${path}"
}

run_query_result() {
  local bin_path="$1"
  local submit_id="$2"
  local tmp_home
  tmp_home="$(mktemp -d)"
  local cfg_dir="${tmp_home}/.dreamina_cli"
  mkdir -p "${cfg_dir}"
  cp "${CREDENTIAL_PATH}" "${cfg_dir}/credential.json"
  local suffix
  for suffix in "" "-shm" "-wal"; do
    if [[ -f "${TASK_DB_PATH}${suffix}" ]]; then
      cp "${TASK_DB_PATH}${suffix}" "${cfg_dir}/tasks.db${suffix}"
    fi
  done
  HOME="${tmp_home}" "${bin_path}" query_result "--submit_id=${submit_id}"
  if [[ "${KEEP_TMP}" != "1" ]]; then
    rm -rf "${tmp_home}"
  else
    echo "[debug] 保留临时目录：${tmp_home}" >&2
  fi
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --submit_id=*)
        SUBMIT_IDS+=("${1#*=}")
        shift
        ;;
      --submit_id)
        [[ $# -ge 2 ]] || { echo "--submit_id 缺少参数" >&2; exit 1; }
        SUBMIT_IDS+=("$2")
        shift 2
        ;;
      --old-bin)
        [[ $# -ge 2 ]] || { echo "--old-bin 缺少参数" >&2; exit 1; }
        OLD_BIN="$2"
        shift 2
        ;;
      --new-bin)
        [[ $# -ge 2 ]] || { echo "--new-bin 缺少参数" >&2; exit 1; }
        NEW_BIN="$2"
        shift 2
        ;;
      --credential)
        [[ $# -ge 2 ]] || { echo "--credential 缺少参数" >&2; exit 1; }
        CREDENTIAL_PATH="$2"
        shift 2
        ;;
      --tasks-db)
        [[ $# -ge 2 ]] || { echo "--tasks-db 缺少参数" >&2; exit 1; }
        TASK_DB_PATH="$2"
        shift 2
        ;;
      --submit_id_file)
        [[ $# -ge 2 ]] || { echo "--submit_id_file 缺少参数" >&2; exit 1; }
        SUBMIT_ID_FILE="$2"
        shift 2
        ;;
      --keep-tmp)
        KEEP_TMP="1"
        shift
        ;;
      --json)
        JSON_STDOUT="1"
        shift
        ;;
      --json-output)
        [[ $# -ge 2 ]] || { echo "--json-output 缺少参数" >&2; exit 1; }
        JSON_OUTPUT="$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        echo "未知参数：$1" >&2
        usage >&2
        exit 1
        ;;
    esac
  done
}

parse_args "$@"

if [[ -n "${SUBMIT_ID_FILE}" ]]; then
  require_file "${SUBMIT_ID_FILE}" "submit_id 文件"
  load_submit_ids_from_file "${SUBMIT_ID_FILE}"
fi

if [[ ${#SUBMIT_IDS[@]} -eq 0 ]]; then
  usage >&2
  exit 1
fi

require_file "${OLD_BIN}" "旧二进制"
require_file "${NEW_BIN}" "新二进制"
require_file "${CREDENTIAL_PATH}" "凭证文件"
require_file "${TASK_DB_PATH}" "任务库"

TMP_SUMMARY_FILE="$(mktemp)"
cleanup_summary() {
  rm -f "${TMP_SUMMARY_FILE}"
}
trap cleanup_summary EXIT

for submit_id in "${SUBMIT_IDS[@]}"; do
  old_json="$(run_query_result "${OLD_BIN}" "${submit_id}")"
  new_json="$(run_query_result "${NEW_BIN}" "${submit_id}")"

  COMPARE_TEXT_OUTPUT="$([[ "${JSON_STDOUT}" == "1" ]] && echo 0 || echo 1)" \
    python3 - "${submit_id}" "${TMP_SUMMARY_FILE}" <<'PY' "${old_json}" "${new_json}"
import json
import sys
import urllib.parse
import os

submit_id = sys.argv[1]
summary_path = sys.argv[2]
old = json.loads(sys.argv[3])
new = json.loads(sys.argv[4])
text_output = os.environ.get("COMPARE_TEXT_OUTPUT", "1") == "1"

def first_media_url(payload):
    result = payload.get("result_json") or {}
    images = result.get("images") or []
    videos = result.get("videos") or []
    if images:
        return "image", images[0].get("image_url")
    if videos:
        return "video", videos[0].get("video_url")
    return "none", None

def url_path(value):
    if not value:
        return None
    return urllib.parse.urlparse(value).path

def normalized_resource_path(value):
    if not value:
        return None
    parsed = urllib.parse.urlparse(value)
    path = parsed.path or ""
    host = (parsed.netloc or "").lower()
    parts = [part for part in path.split("/") if part]
    if "vlabvod.com" in host and len(parts) >= 3:
        first = parts[0]
        if len(first) == 32 and all(ch in "0123456789abcdef" for ch in first.lower()):
            return "/" + "/".join(parts[2:]) + ("/" if path.endswith("/") else "")
    return path

def compare_kind(old_url, new_url):
    if old_url == new_url:
        return "exact_same"
    if normalized_resource_path(old_url) == normalized_resource_path(new_url):
        return "signed_url_only"
    return "different_resource"

media_type, old_url = first_media_url(old)
_, new_url = first_media_url(new)
kind = compare_kind(old_url, new_url)

old_status = old.get("gen_status")
new_status = new.get("gen_status")
old_queue = (old.get("queue_info") or {}).get("queue_status")
new_queue = (new.get("queue_info") or {}).get("queue_status")

item = {
    "submit_id": submit_id,
    "media_type": media_type,
    "compare": kind,
    "old_status": old_status,
    "new_status": new_status,
    "old_queue": old_queue,
    "new_queue": new_queue,
    "old_url": old_url,
    "new_url": new_url,
    "old_path": url_path(old_url),
    "new_path": url_path(new_url),
    "old_resource_path": normalized_resource_path(old_url),
    "new_resource_path": normalized_resource_path(new_url),
}

if text_output:
    print(f"submit_id: {submit_id}")
    print(f"media_type: {media_type}")
    print(f"status: old={old_status} new={new_status}")
    print(f"queue: old={old_queue} new={new_queue}")
    print(f"compare: {kind}")
    print(f"old_url: {old_url}")
    print(f"new_url: {new_url}")
    print(f"old_path: {url_path(old_url)}")
    print(f"new_path: {url_path(new_url)}")
    print(f"old_resource_path: {normalized_resource_path(old_url)}")
    print(f"new_resource_path: {normalized_resource_path(new_url)}")
    print("---")

with open(summary_path, "a", encoding="utf-8") as fh:
    fh.write(json.dumps(item, ensure_ascii=False) + "\n")
PY
done

python3 - "${TMP_SUMMARY_FILE}" "${OLD_BIN}" "${NEW_BIN}" "${CREDENTIAL_PATH}" "${TASK_DB_PATH}" "${SUBMIT_ID_FILE}" "${JSON_STDOUT}" "${JSON_OUTPUT}" <<'PY'
import json
import sys
from collections import Counter

summary_path = sys.argv[1]
old_bin = sys.argv[2]
new_bin = sys.argv[3]
credential_path = sys.argv[4]
tasks_db_path = sys.argv[5]
submit_id_file = sys.argv[6] or None
json_stdout = sys.argv[7] == "1"
json_output = sys.argv[8]

items = []
with open(summary_path, "r", encoding="utf-8") as fh:
    for line in fh:
        line = line.strip()
        if not line:
            continue
        items.append(json.loads(line))

counter = Counter(item["compare"] for item in items)
status_mismatch = sum(1 for item in items if item["old_status"] != item["new_status"])
queue_mismatch = sum(1 for item in items if item["old_queue"] != item["new_queue"])

payload = {
    "old_bin": old_bin,
    "new_bin": new_bin,
    "credential_path": credential_path,
    "tasks_db_path": tasks_db_path,
    "submit_id_file": submit_id_file,
    "items": items,
    "summary": {
        "total": len(items),
        "exact_same": counter.get("exact_same", 0),
        "signed_url_only": counter.get("signed_url_only", 0),
        "different_resource": counter.get("different_resource", 0),
        "status_mismatch": status_mismatch,
        "queue_mismatch": queue_mismatch,
    },
}

if not json_stdout:
    print("summary:")
    print(f"  total: {payload['summary']['total']}")
    for key in ["exact_same", "signed_url_only", "different_resource"]:
        print(f"  {key}: {payload['summary'][key]}")
    print(f"  status_mismatch: {payload['summary']['status_mismatch']}")
    print(f"  queue_mismatch: {payload['summary']['queue_mismatch']}")

if json_output:
    with open(json_output, "w", encoding="utf-8") as fh:
        json.dump(payload, fh, ensure_ascii=False, indent=2)
        fh.write("\n")

if json_stdout:
    print(json.dumps(payload, ensure_ascii=False, indent=2))
PY
