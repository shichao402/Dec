#!/usr/bin/env bash
#
# Dec 代理一键安装（macOS 与 Linux）
#
# 用法：
#   curl -fsSL https://dec.firoyang.com/install.sh | bash
#
# 可覆盖的环境变量：
#   DEC_URL               服务地址，默认 https://dec.firoyang.com
#   DEC_AGENT_DIR         安装目录，默认 ~/.local/bin
#   DEC_AGENT_NAME        设备名，默认 $(hostname)
#   DEC_NONINTERACTIVE    设为 1 时跳过登记（CI、无人值守场景）
#   DEC_INITIALIZE        设为 1 时走 initialize 而不是 enroll（仅首台设备）
#   DEC_NO_TRAY           设为 1 时不安装托盘
#
# 脚本做六件事：判定平台、取服务端要求版本、下载并校验代理与托盘、
# 停掉旧实例并替换二进制、配置服务地址并登记、注册服务与托盘自启动。
#
# 登记这一步由 dec-agent 自己交互完成：主密码从不经过本脚本，
# 也不进命令行参数（会留在 ps 输出与 shell 历史里）。

set -euo pipefail

BASE_URL="${DEC_URL:-https://dec.firoyang.com}"
BASE_URL="${BASE_URL%/}"
INSTALL_DIR="${DEC_AGENT_DIR:-$HOME/.local/bin}"
DEVICE_NAME="${DEC_AGENT_NAME:-$(hostname)}"
AGENT_BIN="$INSTALL_DIR/dec-agent"
PORT=43679

# 状态目录：与 internal/agent/config.go 的 PlatformStateDir 保持一致。
# 显式给了 DEC_AGENT_STATE 就说明这是个隔离实例（测试、调试），
# 此时不得改写全局宿主配置。
if [ -n "${DEC_AGENT_STATE:-}" ]; then
  STATE_DIR="$DEC_AGENT_STATE"
  ISOLATED=1
else
  ISOLATED=0
  if [ "$(uname -s)" = "Linux" ]; then
    STATE_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/dec-agent"
  else
    STATE_DIR="$HOME/.dec-agent"
  fi
fi

C_RESET="" C_DIM="" C_GREEN="" C_YELLOW="" C_RED="" C_CYAN=""
if [ -t 1 ]; then
  C_RESET=$'\033[0m'; C_DIM=$'\033[2m'; C_GREEN=$'\033[32m'
  C_YELLOW=$'\033[33m'; C_RED=$'\033[31m'; C_CYAN=$'\033[36m'
fi

step() { printf '\n%s==>%s %s\n' "$C_CYAN" "$C_RESET" "$1"; }
ok()   { printf '  %sOK%s  %s\n' "$C_GREEN" "$C_RESET" "$1"; }
warn() { printf '  %s!!%s  %s\n' "$C_YELLOW" "$C_RESET" "$1"; }
fail() { printf '  %sXX%s  %s\n' "$C_RED" "$C_RESET" "$1" >&2; }
die()  { fail "$1"; exit 1; }

# 提示信息一律走 stderr：stdout 可能被重定向，而且 stdout 参与不了交互。
note() { printf '%s%s%s\n' "$C_DIM" "$1" "$C_RESET" >&2; }

# platform 形如 darwin-arm64 / linux-amd64，与发布产物名后缀一致。
detect_platform() {
  local os arch
  os="$(uname -s)"
  arch="$(uname -m)"
  case "$os" in
    Darwin) os="darwin" ;;
    Linux)  os="linux" ;;
    *) die "不支持的系统: $os（仅支持 macOS 与 Linux）" ;;
  esac
  # Apple Silicon 在 Rosetta 终端里 uname -m 会报 x86_64，
  # 但 sysctl 能拿到真实架构，此时应按 arm64 装，否则跑不起来。
  if [ "$os" = "darwin" ] && [ "$arch" = "x86_64" ] &&
     [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = "1" ]; then
    arch="arm64"
  fi
  case "$arch" in
    x86_64|amd64)   arch="amd64" ;;
    arm64|aarch64)  arch="arm64" ;;
    *) die "不支持的架构: $arch" ;;
  esac
  printf '%s-%s' "$os" "$arch"
}

# have_tty 判断能不能交互。
#
# curl|bash 时脚本的 stdin 是管道，但 /dev/tty 仍然指向用户的终端；
# 把它重定向给 dec-agent，它内部的 term.IsTerminal 就会返回真，
# 无回显读密码正常工作。实测确认过这条路径。
have_tty() { [ -r /dev/tty ] && (exec 3</dev/tty) 2>/dev/null; }

# fetch_required 从健康检查取要求版本。
#
# /healthz 是无证书公开端点，返回 required_client，比抓下载页的 HTML 稳。
# 取不到不算错：服务端可能还没上传清单、或此处是旧版本服务端，
# 此时按当前版本装即可，绝不因此中断安装。
#
# 末尾的 `|| true` 是必需的：set -euo pipefail 下，curl 失败会让整条
# 管道的返回码非 0，命令替换又会把这个非 0 传给外层的赋值，
# 于是脚本在这里静默退出——用户只会看到输出突然断掉。
fetch_required() {
  command -v curl >/dev/null 2>&1 || return 0
  curl -fsS --max-time 20 "$BASE_URL/healthz" 2>/dev/null |
    tr ',{}' '\n\n\n' | sed -n 's/.*"required_client"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
    head -n1 || true
}

# installed_version 读已装代理的版本；没装或跑不起来时返回空。
#
# 同样需要 `|| true`：未安装时 `[ -x ... ] || return 0` 已经返回 0，
# 但装了却跑不起来的场合，grep 拿不到匹配会以 1 退出。
installed_version() {
  [ -x "$AGENT_BIN" ] || return 0
  "$AGENT_BIN" version 2>/dev/null | grep -Eo 'v[0-9]+\.[0-9]+\.[0-9]+' | head -n1 || true
}

# download_verified 下载产物并用响应头给出的摘要校验。
#
# 摘要由服务端从发布清单里算好放进 X-Checksum-SHA256，
# 注入式远程安装下静默接受未校验的二进制是不可接受的风险。
# Optional 产物（托盘）下载失败只告警，不阻断。
download_verified() {
  local url="$1" dest="$2" optional="${3:-}"
  local expected actual tmp
  # 同样要 `|| true`：curl 失败（含自签证书未信任）在 pipefail 下会让
  # 整条管道非 0，命令替换把它带出来就会直接终止脚本。
  expected="$(curl -fsSI --max-time 30 "$url" 2>/dev/null |
    tr -d '\r' | sed -n 's/^[Xx]-[Cc]hecksum-[Ss][Hh][Aa]256:[[:space:]]*//p' |
    head -n1 | tr 'A-Z' 'a-z' || true)"
  if [ -z "$expected" ] && [ "$optional" != "optional" ]; then
    warn "服务端未提供摘要，将跳过完整性校验: $(basename "$dest")"
  fi
  tmp="$dest.download"
  rm -f "$tmp"
  # 可选产物下载失败是预期情况（本次可能没发这个平台的托盘），
  # 所以吞掉 curl 自己的报错，只留脚本的中文说明。
  if ! curl -fsSL --max-time 300 -o "$tmp" "$url" 2>/dev/null; then
    rm -f "$tmp"
    [ "$optional" = "optional" ] && return 1
    die "下载失败: $url"
  fi
  if [ -n "$expected" ]; then
    actual="$(sha256_file "$tmp")"
    if [ "$actual" != "$expected" ]; then
      rm -f "$tmp"
      fail "产物校验失败: $url"
      fail "  期望 sha256: $expected"
      fail "  实际 sha256: $actual"
      fail "已删除该产物。请勿使用来源不明的二进制。"
      exit 1
    fi
    ok "校验通过: $(basename "$dest")"
  fi
  chmod +x "$tmp"
  mv -f "$tmp" "$dest"
  return 0
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    # macOS 没有 sha256sum。
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

# strip_quarantine 移除 macOS 的隔离标记。
#
# curl 下载的文件会被打上 com.apple.quarantine，首次运行被 Gatekeeper 拦下，
# 报错信息还指向"磁盘映像"之类无关原因。二进制不做代码签名（本项目只有一个
# 使用者），所以这里直接清标记，省掉用户去 Finder 右键那一步。
strip_quarantine() {
  [ "$(uname -s)" = "Darwin" ] || return 0
  command -v xattr >/dev/null 2>&1 || return 0
  xattr -d com.apple.quarantine "$1" 2>/dev/null || true
}

stop_existing() {
  # launchd 与 systemctl 都可能不存在，静默忽略：装了才算，没装不用管。
  if [ "$(uname -s)" = "Darwin" ]; then
    launchctl kickstart -k "gui/$(id -u)/com.dec.agent" 2>/dev/null || true
    launchctl bootout "gui/$(id -u)/com.dec.agent" 2>/dev/null || true
  else
    systemctl --user stop dec-agent.service 2>/dev/null || true
  fi
  pkill -x dec-agent 2>/dev/null || true
  pkill -x dec-tray 2>/dev/null || true
  sleep 1
}

add_to_path() {
  # 隔离实例不碰 shell 配置：那是全局状态，测试跑一次就污染一次。
  [ "$ISOLATED" = "1" ] && return 0
  # 只在目录确实不在 PATH 里时提示，避免每次升级都刷一遍。
  case ":$PATH:" in
    *":$INSTALL_DIR:"*) return 0 ;;
  esac
  local rc
  rc="$(shell_rc)"
  [ -n "$rc" ] || return 0
  if ! grep -Fq "$INSTALL_DIR" "$rc" 2>/dev/null; then
    printf '\nexport PATH="%s:$PATH"\n' "$INSTALL_DIR" >>"$rc"
    ok "已把 $INSTALL_DIR 写入 $rc"
    note "新开一个终端即可生效，或执行: source $rc"
  fi
}

shell_rc() {
  case "${SHELL:-}" in
    */zsh)  printf '%s/.zshrc' "$HOME" ;;
    */bash) printf '%s/.bashrc' "$HOME" ;;
    *)      printf '%s/.profile' "$HOME" ;;
  esac
}

main() {
  command -v curl >/dev/null 2>&1 || die "需要 curl，请先安装"

  local platform required current
  platform="$(detect_platform)"
  step "检查环境"
  note "平台      $platform"
  note "安装目录  $INSTALL_DIR"
  note "服务地址  $BASE_URL"
  note "设备名    $DEVICE_NAME"

  step "检查版本"
  required="$(fetch_required)"
  current="$(installed_version)"
  if [ -n "$required" ]; then
    note "服务端要求 $required"
    if [ "$current" = "$required" ]; then
      ok "本机已是 $current"
      if [ "${DEC_FORCE:-}" != "1" ]; then
        note "已是要求版本，跳过下载。重跑请设 DEC_FORCE=1。"
      fi
    fi
  fi

  step "下载并校验"
  mkdir -p "$INSTALL_DIR"
  if [ "$current" = "$required" ] && [ -n "$required" ] && [ "${DEC_FORCE:-}" != "1" ]; then
    ok "跳过下载（已是要求版本）"
  else
    download_verified "$BASE_URL/download/dec-agent-$platform" "$AGENT_BIN"
    strip_quarantine "$AGENT_BIN"
  fi

  # 托盘是可选组件：缺失时代理照常工作。
  if [ "${DEC_NO_TRAY:-}" != "1" ]; then
    if download_verified "$BASE_URL/download/dec-tray-$platform" "$INSTALL_DIR/dec-tray" optional; then
      strip_quarantine "$INSTALL_DIR/dec-tray"
    else
      note "本次没有适合本平台的托盘产物，跳过（代理照常工作）"
    fi
  fi

  step "替换二进制"
  stop_existing
  if ! "$AGENT_BIN" version >/dev/null 2>&1; then
    die "下载产物无法运行: $AGENT_BIN"
  fi
  ok "$("$AGENT_BIN" version | head -n1)"

  step "配置服务地址"
  # configure 在地址未变时不动本机身份；地址变了才会清掉并要求重新登记。
  "$AGENT_BIN" configure --url "$BASE_URL" >/dev/null || die "配置服务地址失败"
  ok "$BASE_URL"

  step "登记这台设备"
  if [ -f "$STATE_DIR/identity/identity.json" ]; then
    ok "本机已登记，跳过"
  elif [ "${DEC_NONINTERACTIVE:-}" = "1" ] || ! have_tty; then
    warn "非交互模式，跳过登记。稍后手动执行："
    note "  $AGENT_BIN enroll --name \"$DEVICE_NAME\""
  else
    note "需要 Bitwarden 邮箱、主密码与两步验证码。"
    note "输入由 dec-agent 直接读取，不经过本脚本。"
    local sub="enroll"
    [ "${DEC_INITIALIZE:-}" = "1" ] && sub="initialize"
    # stdin 重定向到 /dev/tty：脚本自己的 stdin 是管道，
    # 不这样做 dec-agent 会立刻读到 EOF 而拿到空凭据。
    if "$AGENT_BIN" "$sub" --name "$DEVICE_NAME" < /dev/tty; then
      ok "登记完成"
    else
      warn "登记未完成。可稍后重跑："
      note "  $AGENT_BIN enroll --name \"$DEVICE_NAME\""
    fi
  fi

  step "注册服务"
  # 显式传 --binary 与 --tray：install 默认取"当前进程"，
  # 从脚本里调用时那条路径是 bash 而不是安装位置。
  if [ "$ISOLATED" = "1" ]; then
    # 隔离实例不注册系统服务：那会覆盖本机正在服役的那个实例。
    note "隔离实例（DEC_AGENT_STATE 已设置），跳过服务注册"
  elif [ -x "$INSTALL_DIR/dec-tray" ]; then
    "$AGENT_BIN" install --binary "$AGENT_BIN" --tray "$INSTALL_DIR/dec-tray" ||
      die "注册服务失败"
    ok "已注册（不需要 sudo）"
  else
    "$AGENT_BIN" install --binary "$AGENT_BIN" --no-tray || die "注册服务失败"
    ok "已注册（不需要 sudo）"
  fi

  add_to_path

  step "验证"
  local deadline=$(( $(date +%s) + 40 )) status=""
  while [ "$(date +%s)" -lt "$deadline" ]; do
    status="$(curl -fsS --max-time 3 "http://127.0.0.1:$PORT/api/status" 2>/dev/null || true)"
    [ -n "$status" ] && break
    sleep 2
  done
  if [ -z "$status" ]; then
    warn "本机页面尚未响应，服务可能还在启动。查看日志：$STATE_DIR/logs"
    exit 1
  fi
  ok "代理在线: $(printf '%s' "$status" | sed -n 's/.*"server_url"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
  printf '%s' "$status" | sed -n 's/.*"device_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/  设备 \1/p' | head -n1
  if printf '%s' "$status" | grep -q '"aligned"[[:space:]]*:[[:space:]]*false'; then
    warn "代理版本与服务端不对齐，请重跑本脚本升级"
  fi

  printf '\n'
  note "管理面  http://127.0.0.1:$PORT/admin"
  note "卸载    $AGENT_BIN uninstall"
  printf '\n'
}

main "$@"
