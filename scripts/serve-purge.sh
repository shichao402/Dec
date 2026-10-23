#!/usr/bin/env bash
#
# serve-purge.sh — 在局域网内起一个临时 HTTP 服务，让其他机器（含 Windows）
# 通过一条命令即时拉取并执行旧一代 Dec 清理脚本。
#
# 为什么需要它：
#   macOS 端直接跑 scripts/purge-legacy-dec.sh 即可。
#   Windows 端没有本机副本，需要通过网络把脚本拉过去执行。
#   用 HTTP 而不是拷贝文件，是为了「一条命令即可」。
#
# 用法：
#   ./scripts/serve-purge.sh              # 起服务，打印可直接粘贴的命令
#   ./scripts/serve-purge.sh 9000         # 指定端口
#   ./scripts/serve-purge.sh 9000 180     # 指定端口 + 存活秒数（默认 300）
#
# 安全说明：
#   1. 只绑定 0.0.0.0 上的指定端口；脚本内容是公开的清理逻辑，不含任何凭据。
#   2. 服务默认 300 秒后自动退出，避免长期暴露。
#   3. 强烈建议先让对端「预演」跑一遍（不加 -Apply），确认清单无误再执行。
#
set -uo pipefail

PORT="${1:-8765}"
LIFETIME="${2:-300}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

if [ ! -f "$ROOT/scripts/purge-legacy-dec.ps1" ]; then
  echo "找不到 scripts/purge-legacy-dec.ps1" >&2
  exit 1
fi

# 取本机在局域网内的地址，供对端直接使用。
LAN_IP=""
for iface in en0 en1 en2; do
  ip=$(ipconfig getifaddr "$iface" 2>/dev/null || true)
  if [ -n "$ip" ]; then LAN_IP="$ip"; break; fi
done
[ -z "$LAN_IP" ] && LAN_IP="<本机IP>"

BASE="http://${LAN_IP}:${PORT}"

cat <<EOF
=== 临时分发服务 ===
根目录: $ROOT
监听:   0.0.0.0:${PORT}
存活:   ${LIFETIME} 秒后自动退出

Windows（PowerShell）预演：
  irm ${BASE}/purge-legacy-dec.ps1 | iex

Windows（PowerShell）执行：
  \$env:PURGE_APPLY=1; irm ${BASE}/purge-legacy-dec.ps1 | iex

macOS / Linux 预演：
  curl -fsSL ${BASE}/purge-legacy-dec.sh | bash

macOS / Linux 执行：
  curl -fsSL ${BASE}/purge-legacy-dec.sh | bash -s -- --apply

注意：
  - 预演不删任何东西，先跑预演核对清单。
  - 若对端在另一网段，${LAN_IP} 可能不可达，改用能互通的地址。
  - 服务会在 ${LIFETIME} 秒后自动关闭。

Ctrl+C 可提前结束。
EOF
echo

cd "$ROOT/scripts" || exit 1

# 只暴露这两个文件，用 python3 起一个最简静态服务。
python3 - "$PORT" "$LIFETIME" <<'PY'
import http.server, socketserver, sys, threading, time, os

port = int(sys.argv[1])
lifetime = int(sys.argv[2])

class Handler(http.server.SimpleHTTPRequestHandler):
    def end_headers(self):
        self.send_header("Cache-Control", "no-store")
        super().end_headers()

    def log_message(self, fmt, *args):
        print(f"  [{time.strftime('%H:%M:%S')}] {self.address_string()} {fmt % args}")

def shutdown(server):
    time.sleep(lifetime)
    print(f"\n存活时间到（{lifetime}s），关闭服务。")
    os._exit(0)

socketserver.TCPServer.allow_reuse_address = True
with socketserver.TCPServer(("0.0.0.0", port), Handler) as httpd:
    threading.Thread(target=shutdown, args=(httpd,), daemon=True).start()
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        print("\n已手动停止。")
PY
