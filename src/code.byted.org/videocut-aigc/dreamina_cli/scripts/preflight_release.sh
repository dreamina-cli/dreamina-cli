#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
BUILD_SCRIPT="${ROOT_DIR}/scripts/build_release.sh"
COMPARE_SCRIPT="${ROOT_DIR}/scripts/compare_legacy_query_result.sh"
DEFAULT_SAMPLE_FILE="${ROOT_DIR}/testdata/legacy/query_result_submit_ids.txt"
DEFAULT_OLD_BIN="${HOME}/.local/bin/dreamina"

SKIP_BUILD="0"
RUN_COMPARE="1"
OLD_BIN="${DEFAULT_OLD_BIN}"
SAMPLE_FILE="${DEFAULT_SAMPLE_FILE}"
JSON_OUTPUT=""
TARGETS=()

usage() {
  cat <<EOF
用法：
  bash scripts/preflight_release.sh
  bash scripts/preflight_release.sh --target darwin/arm64 --target darwin/amd64

可选参数：
  --target <goos/goarch>    指定构建目标，可重复；默认使用当前平台
  --skip-build              跳过构建，只做版本检查与 query_result 对比
  --skip-compare            跳过 query_result 对比，只做构建与版本检查
  --old-bin <path>          旧二进制路径，默认：${DEFAULT_OLD_BIN}
  --sample-file <path>      query_result 对比样本文件，默认：${DEFAULT_SAMPLE_FILE}
  --json-output <path>      写出 preflight JSON 结果，便于 CI 或脚本消费
  -h, --help                显示帮助

说明：
  1. 默认会先构建当前平台产物，使 dist/current/dreamina 指向最新构建。
  2. 然后输出新二进制 version。
  3. 最后使用 compare_legacy_query_result.sh 跑固定样本清单。
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

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --target)
        [[ $# -ge 2 ]] || { echo "--target 缺少参数" >&2; exit 1; }
        TARGETS+=("$2")
        shift 2
        ;;
      --skip-build)
        SKIP_BUILD="1"
        shift
        ;;
      --skip-compare)
        RUN_COMPARE="0"
        shift
        ;;
      --old-bin)
        [[ $# -ge 2 ]] || { echo "--old-bin 缺少参数" >&2; exit 1; }
        OLD_BIN="$2"
        shift 2
        ;;
      --sample-file)
        [[ $# -ge 2 ]] || { echo "--sample-file 缺少参数" >&2; exit 1; }
        SAMPLE_FILE="$2"
        shift 2
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

if [[ ${#TARGETS[@]} -eq 0 ]]; then
  if command -v go >/dev/null 2>&1; then
    host_goos="$(go env GOOS 2>/dev/null || true)"
    host_goarch="$(go env GOARCH 2>/dev/null || true)"
    if [[ -n "${host_goos}" && -n "${host_goarch}" ]]; then
      TARGETS+=("${host_goos}/${host_goarch}")
    fi
  fi
fi

if [[ ${#TARGETS[@]} -eq 0 ]]; then
  echo "无法自动确定当前平台，请显式传入 --target" >&2
  exit 1
fi

require_file "${BUILD_SCRIPT}" "构建脚本"
require_file "${COMPARE_SCRIPT}" "对比脚本"
require_file "${OLD_BIN}" "旧二进制"
require_file "${SAMPLE_FILE}" "样本文件"

TMP_COMPARE_JSON="$(mktemp)"
cleanup_tmp() {
  rm -f "${TMP_COMPARE_JSON}"
}
trap cleanup_tmp EXIT

echo "==> preflight targets: ${TARGETS[*]}"

if [[ "${SKIP_BUILD}" != "1" ]]; then
  echo "==> build release targets"
  GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" bash "${BUILD_SCRIPT}" "${TARGETS[@]}"
fi

NEW_BIN="${ROOT_DIR}/dist/current/dreamina"
if [[ ! -x "${NEW_BIN}" ]]; then
  echo "dist/current/dreamina 不存在，尝试回退到首个目标产物" >&2
  first_target="${TARGETS[0]}"
  target_goos="${first_target%%/*}"
  target_goarch="${first_target##*/}"
  NEW_BIN="${ROOT_DIR}/dist/${target_goos}-${target_goarch}/dreamina"
fi
require_file "${NEW_BIN}" "新二进制"

echo "==> new binary version"
VERSION_OUTPUT="$("${NEW_BIN}" version)"
printf '%s\n' "${VERSION_OUTPUT}"

PRECHECK_FAILED="0"
if [[ "${RUN_COMPARE}" == "1" ]]; then
  echo "==> compare legacy query_result samples"
  bash "${COMPARE_SCRIPT}" \
    --old-bin "${OLD_BIN}" \
    --new-bin "${NEW_BIN}" \
    --submit_id_file "${SAMPLE_FILE}" \
    --json-output "${TMP_COMPARE_JSON}"

  if ! python3 - "${TMP_COMPARE_JSON}" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as fh:
    payload = json.load(fh)

summary = payload.get("summary") or {}
failed = any(summary.get(key, 0) > 0 for key in [
    "different_resource",
    "status_mismatch",
    "queue_mismatch",
])
raise SystemExit(1 if failed else 0)
PY
  then
    PRECHECK_FAILED="1"
    echo "preflight 失败：query_result 存在资源、状态或队列不一致" >&2
  fi
fi

if [[ -n "${JSON_OUTPUT}" ]]; then
  python3 - "${JSON_OUTPUT}" "${OLD_BIN}" "${NEW_BIN}" "${SAMPLE_FILE}" "${VERSION_OUTPUT}" "${SKIP_BUILD}" "${RUN_COMPARE}" "${PRECHECK_FAILED}" "${TMP_COMPARE_JSON}" "${TARGETS[@]}" <<'PY'
import json
import sys

json_output = sys.argv[1]
old_bin = sys.argv[2]
new_bin = sys.argv[3]
sample_file = sys.argv[4]
version_output = sys.argv[5]
skip_build = sys.argv[6] == "1"
run_compare = sys.argv[7] == "1"
failed = sys.argv[8] == "1"
compare_json_path = sys.argv[9]
targets = sys.argv[10:]

try:
    version_data = json.loads(version_output)
except json.JSONDecodeError:
    version_data = None

compare_data = None
if run_compare:
    with open(compare_json_path, "r", encoding="utf-8") as fh:
        compare_data = json.load(fh)

payload = {
    "targets": targets,
    "skip_build": skip_build,
    "run_compare": run_compare,
    "failed": failed,
    "old_bin": old_bin,
    "new_bin": new_bin,
    "sample_file": sample_file,
    "version_output": version_output,
    "version_data": version_data,
    "compare": compare_data,
}

with open(json_output, "w", encoding="utf-8") as fh:
    json.dump(payload, fh, ensure_ascii=False, indent=2)
    fh.write("\n")
PY
fi

if [[ "${PRECHECK_FAILED}" == "1" ]]; then
  exit 1
fi
