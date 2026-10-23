#!/usr/bin/env bash
#
# 真实链路验收（需求 4.1、4.2）。
#
# 为什么需要这个脚本：单元测试用的是假云端，能证明逻辑对，不能证明
# 「用户此刻真的能用」。它有两种模式，验的是两件不同的事：
#
#   本机模式（默认）：对本机运行中的代理跑完整链路——MCP 握手、工具清单、
#     写入一个临时 bundle 并确认它落到这台机器的磁盘上、再删掉。
#     这条链路必须有代理，因为 MCP 入口就在代理里。
#
#   线上模式（--remote）：把生产服务器上「此刻的事实」逐条查一遍——
#     服务在不在、公网开了哪些端口、证书是谁签的还剩几天、无证书调用是否
#     都被拒、资产库里有没有验收残留。这些结论只有在那台机器上才拿得到，
#     本机跑看到的只是副本。
#
# 用法：
#   scripts/acceptance.sh                          # 本机完整链路
#   scripts/acceptance.sh --remote                 # 对生产服务器做线上验收
#   scripts/acceptance.sh --remote --host decops@dec.firoyang.com
#   scripts/acceptance.sh --binary ./dec-agent
#   scripts/acceptance.sh --url http://127.0.0.1:8787
#   scripts/acceptance.sh --keep                   # 保留测试数据，便于人工观察
#
# 退出码：
#   0  全部通过且无残留
#   1  有检查未通过
#   2  环境问题（找不到二进制、代理没起来、连不上服务器）
#   3  检查通过但有残留项
set -uo pipefail

BASE_URL="http://127.0.0.1:8787"
BINARY=""
BUNDLE=""
PROJECT=""
TIMEOUT="90s"
KEEP=""
REMOTE=""
REMOTE_HOST="decops@dec.firoyang.com"
REMOTE_PORT="22"
REMOTE_KEY=""
DOMAIN="dec.firoyang.com"

show_help() {
  sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
}

while [ $# -gt 0 ]; do
  case "$1" in
    --url) BASE_URL="$2"; shift 2 ;;
    --binary) BINARY="$2"; shift 2 ;;
    --bundle) BUNDLE="$2"; shift 2 ;;
    --project) PROJECT="$2"; shift 2 ;;
    --timeout) TIMEOUT="$2"; shift 2 ;;
    --keep) KEEP="--keep"; shift ;;
    --remote) REMOTE=1; shift ;;
    --host) REMOTE_HOST="$2"; shift 2 ;;
    --port) REMOTE_PORT="$2"; shift 2 ;;
    --key) REMOTE_KEY="$2"; shift 2 ;;
    --domain) DOMAIN="$2"; shift 2 ;;
    -h|--help) show_help; exit 0 ;;
    *) echo "未知参数：$1" >&2; echo >&2; show_help >&2; exit 2 ;;
  esac
done

# run_remote 把 acceptance-prod.sh 送到服务器上执行。
#
# 用 bash -s 从 stdin 读脚本而不是先 scp：不留文件、不需要临时目录，
# 脚本内容就是本地这份（唯一事实来源），不会出现「服务器上那份是旧的」。
run_remote() {
  local here
  here="$(cd "$(dirname "$0")" && pwd)"
  local script="$here/acceptance-prod.sh"
  if [ ! -f "$script" ]; then
    echo "找不到 $script" >&2
    exit 2
  fi

  local ssh_args=(-o BatchMode=yes -o ConnectTimeout=15 -o ServerAliveInterval=15)
  # host key 必须已知。首次连接就自动接受等于把中间人攻击的门留着，
  # 所以这里用 StrictHostKeyChecking=yes，失败就明确报出来让人处理。
  ssh_args+=(-o StrictHostKeyChecking=yes -p "$REMOTE_PORT")
  [ -n "$REMOTE_KEY" ] && ssh_args+=(-i "$REMOTE_KEY")

  echo "线上验收目标：${REMOTE_HOST}:${REMOTE_PORT}（域名 ${DOMAIN}）"
  echo

  # 远端只需要有 bash、curl、python3、openssl、sha256sum、sudo。
  # 缺命令时脚本自己的 need/info 会把那条算成跳过或失败，不会静默通过。
  # shellcheck disable=SC2029
  ssh "${ssh_args[@]}" "$REMOTE_HOST" "bash -s -- '$DOMAIN'" < "$script"
  local status=$?

  echo
  case "$status" in
    0) echo "线上验收通过。" ;;
    255)
      echo "连不上服务器，或 host key 未知。" >&2
      echo "先确认 ssh $REMOTE_HOST 能通；若是首次连接，用 ssh-keyscan 把 host key 加进 known_hosts。" >&2
      exit 2
      ;;
    *)
      echo "线上验收有未通过项（退出码 ${status}）。" >&2
      exit 1
      ;;
  esac
}

if [ -n "$REMOTE" ]; then
  run_remote
  exit $?
fi

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
  echo "只想验线上服务端的话，用 --remote。" >&2
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
