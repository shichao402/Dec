#!/usr/bin/env bash
#
# 真实链路验收（需求 4.1、4.2）。
#
# 为什么需要这个脚本：单元测试用的是假云端，能证明逻辑对，不能证明
# 「用户此刻真的能用」。这个脚本对本机运行中的代理和生产云端跑一遍完整
# 链路，任何一项不通过就退出非 0，并且无论成败都会尝试清理测试数据。
#
# 用法：
#   scripts/acceptance.sh                  # 用已安装的 dec-agent
#   scripts/acceptance.sh --binary ./dec-agent
#   scripts/acceptance.sh --url http://127.0.0.1:8787
#   scripts/acceptance.sh --keep            # 保留测试数据，便于人工观察
#
# 退出码：
#   0  全部通过且无残留
#   1  有检查未通过
#   2  环境问题（找不到二进制、代理没起来）
#   3  检查通过但有残留项
set -uo pipefail

BASE_URL="http://127.0.0.1:8787"
BINARY=""
BUNDLE=""
PROJECT=""
TIMEOUT="90s"
KEEP=""

show_help() {
  sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
}

while [ $# -gt 0 ]; do
  case "$1" in
    --url) BASE_URL="$2"; shift 2 ;;
    --binary) BINARY="$2"; shift 2 ;;
    --bundle) BUNDLE="$2"; shift 2 ;;
    --project) PROJECT="$2"; shift 2 ;;
    --timeout) TIMEOUT="$2"; shift 2 ;;
    --keep) KEEP="--keep"; shift ;;
    -h|--help) show_help; exit 0 ;;
    *) echo "未知参数：$1" >&2; echo >&2; show_help >&2; exit 2 ;;
  esac
done

# 找到要用的二进制。优先用参数指定的，其次用 PATH 里的，最后用仓库里刚构建的。
if [ -z "$BINARY" ]; then
  if command -v dec-agent >/dev/null 2>&1; then
    BINARY="$(command -v dec-agent)"
  elif [ -x "./dec-agent" ]; then
    BINARY="./dec-agent"
  elif [ -x "bin/dec-agent" ]; then
    BINARY="bin/dec-agent"
  else
    echo "找不到 dec-agent。请先安装，或用 --binary 指定路径。" >&2
    exit 2
  fi
fi
if [ ! -x "$BINARY" ]; then
  echo "二进制不可执行：$BINARY" >&2
  exit 2
fi

echo "使用二进制：$BINARY"
if ! "$BINARY" version >/dev/null 2>&1; then
  echo "二进制无法运行：$BINARY" >&2
  exit 2
fi
"$BINARY" version | sed 's/^/  /'

# 代理必须已经在跑：验收针对的是运行中的进程，不是仓库里的代码。
# 这里只用 /api/status 探一下，具体检查交给验收客户端，避免两处判定不一致。
if ! curl -fsS --max-time 10 "$BASE_URL/api/status" >/dev/null 2>&1; then
  echo "代理没有响应：$BASE_URL/api/status" >&2
  echo "先确认服务在跑（launchctl list | grep com.dec.agent，或 dec-agent run）。" >&2
  exit 2
fi

ARGS=(acceptance --url "$BASE_URL" --timeout "$TIMEOUT")
[ -n "$BUNDLE" ] && ARGS+=(--bundle "$BUNDLE")
[ -n "$PROJECT" ] && ARGS+=(--project "$PROJECT")
[ -n "$KEEP" ] && ARGS+=("$KEEP")

"$BINARY" "${ARGS[@]}"
STATUS=$?

case "$STATUS" in
  0) exit 0 ;;
  1) exit 1 ;;
  *)
    # 其它退出码来自二进制本身（例如 flag 解析失败），按环境问题处理。
    if [ "$STATUS" -eq 3 ]; then
      exit 3
    fi
    echo "验收客户端异常退出：$STATUS" >&2
    exit 2
    ;;
esac
