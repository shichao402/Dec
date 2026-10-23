#!/usr/bin/env bash
#
# purge-legacy-dec.sh — 断代清理旧一代 Dec 在本机留下的全部产物。
#
# 只清理旧一代：Console / dec-server / dec-exec / dec-mcp / ~/.dec /
# 项目内 .dec 与落点 / IDE 里的 dec-* 条目。
# 新架构（dec-agent + ~/.dec-agent + com.dec.agent.plist）被硬编码保护，一步不碰。
#
# 用法：
#   bash purge-legacy-dec.sh                     # 预演，只打印清单
#   bash purge-legacy-dec.sh --apply             # 执行
#   bash purge-legacy-dec.sh --apply --purge-secrets
#                                                # 连 .secrets/ 一起删（默认保留）
#
# 通过网络即时执行：
#   curl -fsSL http://<host>:<port>/purge-legacy-dec.sh | bash
#   curl -fsSL http://<host>:<port>/purge-legacy-dec.sh | bash -s -- --apply
#
# 判定依据：旧架构落点带 dec- 前缀，新架构不带（如 relkit-ops），互不冲突。
#
set -uo pipefail

APPLY=0
PURGE_SECRETS=0
WORKSPACE="${DEC_WORKSPACE:-$HOME/workspace}"

show_help() {
  cat <<'HELP'
旧一代 Dec 清理脚本

用法：
  purge-legacy-dec.sh [选项]

选项：
  --apply           真正执行删除。不加则只预演，打印待删清单。
  --purge-secrets   连各项目 .secrets/ 一起删。默认保留（里面是凭据，删了不可恢复）。
  -h, --help        显示本帮助。

环境变量：
  DEC_WORKSPACE     项目根目录，默认 ~/workspace。

通过网络执行：
  curl -fsSL <url> | bash
  curl -fsSL <url> | bash -s -- --apply

清理范围（旧一代）：
  ~/.dec/                        全局状态、repo.git、bin、cache、runtime-cache、secrets
  /Applications/dec-console.app  旧 Console 应用
  ~/Library/Application Support/com.shichao402.dec.console
  ~/Library/Caches/com.shichao402.dec.console
  ~/Library/Preferences/com.shichao402.dec.console.plist
  <proj>/.dec/                   项目内旧状态
  各项目 IDE 目录中 dec-* 开头的 skill / rule
  各 IDE MCP 配置中的 dec-* 条目

保留（新架构）：
  ~/.dec-agent/
  ~/Library/LaunchAgents/com.dec.agent.plist
  ~/.local/bin/dec-agent
  各项目 IDE 目录中不带 dec- 前缀的条目
HELP
}

for arg in "$@"; do
  case "$arg" in
    --apply) APPLY=1 ;;
    --purge-secrets) PURGE_SECRETS=1 ;;
    -h|--help) show_help; exit 0 ;;
    *)
      echo "未知参数: ${arg}" >&2
      echo "用 --help 查看用法。" >&2
      exit 2
      ;;
  esac
done

if [ "$APPLY" -eq 1 ]; then MODE="执行"; else MODE="预演"; fi

REMOVED=0
SKIPPED=0
FAILED=0

# 新代理的 PID，任何情况下不动。
NEW_AGENT_PIDS=$(pgrep -f "dec-agent" 2>/dev/null | tr '\n' ' ' || true)

purge_path() {
  local label="$1" path="$2"

  if [ ! -e "$path" ] && [ ! -L "$path" ]; then
    return 0
  fi

  case "$path" in
    "$HOME/.dec-agent"|"$HOME/.dec-agent/"*)
      echo "  [保护] $label -> $path"
      SKIPPED=$((SKIPPED + 1))
      return 0
      ;;
    *"com.dec.agent.plist")
      echo "  [保护] $label -> $path"
      SKIPPED=$((SKIPPED + 1))
      return 0
      ;;
  esac

  if [ "$APPLY" -eq 0 ]; then
    echo "  [待删] $label -> $path"
    REMOVED=$((REMOVED + 1))
    return 0
  fi

  if rm -rf "$path" 2>/dev/null; then
    echo "  [已删] $label -> $path"
    REMOVED=$((REMOVED + 1))
  else
    echo "  [失败] $label -> $path" >&2
    FAILED=$((FAILED + 1))
  fi
}

echo "=== 旧一代 Dec 清理（${MODE}）==="
echo "工作区: $WORKSPACE"
echo

# ---------------------------------------------------------------------------
# 1. 停掉旧进程
# ---------------------------------------------------------------------------
echo "--- 1. 旧进程 ---"
LEGACY_PIDS=""

if [ -d "/Applications/dec-console.app" ]; then
  LEGACY_PIDS="$LEGACY_PIDS $(pgrep -f "/Applications/dec-console.app/Contents/MacOS/dec-console" 2>/dev/null || true)"
fi
if [ -d "$HOME/.dec/bin" ]; then
  LEGACY_PIDS="$LEGACY_PIDS $(pgrep -f "$HOME/.dec/bin/dec-" 2>/dev/null || true)"
fi

LEGACY_PIDS=$(echo "$LEGACY_PIDS" | tr ' ' '\n' | grep -E '^[0-9]+$' | sort -u | tr '\n' ' ' || true)

if [ -z "$(echo "$LEGACY_PIDS" | tr -d ' ')" ]; then
  echo "  没有发现旧进程"
else
  for pid in $LEGACY_PIDS; do
    protected=0
    for np in $NEW_AGENT_PIDS; do
      if [ "$pid" = "$np" ]; then protected=1; break; fi
    done
    if [ "$protected" -eq 1 ]; then
      echo "  [保护] 跳过新代理 PID $pid"
      SKIPPED=$((SKIPPED + 1))
      continue
    fi

    if [ "$APPLY" -eq 0 ]; then
      echo "  [待停] PID $pid ($(ps -p "$pid" -o command= 2>/dev/null | cut -c1-70))"
    else
      if kill "$pid" 2>/dev/null; then
        echo "  [已停] PID $pid"
      else
        echo "  [失败] 无法停止 PID $pid" >&2
        FAILED=$((FAILED + 1))
      fi
    fi
  done
fi
echo

# ---------------------------------------------------------------------------
# 2. 旧全局状态
# ---------------------------------------------------------------------------
echo "--- 2. 旧全局状态 ---"
purge_path "旧全局状态 ~/.dec" "$HOME/.dec"
echo

# ---------------------------------------------------------------------------
# 3. 旧 Console
# ---------------------------------------------------------------------------
echo "--- 3. 旧 Console ---"
purge_path "dec-console.app" "/Applications/dec-console.app"
purge_path "Console 数据" "$HOME/Library/Application Support/com.shichao402.dec.console"
purge_path "Console 数据(旧名)" "$HOME/Library/Application Support/dec-console"
purge_path "Console 缓存" "$HOME/Library/Caches/com.shichao402.dec.console"
purge_path "Console 偏好" "$HOME/Library/Preferences/com.shichao402.dec.console.plist"
echo

# ---------------------------------------------------------------------------
# 4. 各项目内旧 .dec
# ---------------------------------------------------------------------------
echo "--- 4. 项目内 .dec 目录 ---"
if [ -d "$WORKSPACE" ]; then
  for proj in "$WORKSPACE"/*/; do
    [ -d "$proj" ] || continue
    purge_path "项目 .dec" "${proj}.dec"
  done
  echo "  （共 $(ls -d "$WORKSPACE"/*/.dec 2>/dev/null | wc -l | tr -d ' ') 个）"
else
  echo "  工作区不存在: $WORKSPACE"
fi
echo

# ---------------------------------------------------------------------------
# 5. 各项目内 .secrets
# ---------------------------------------------------------------------------
echo "--- 5. 项目内 .secrets 目录 ---"
if [ "$PURGE_SECRETS" -eq 0 ]; then
  n=$(ls -d "$WORKSPACE"/*/.secrets 2>/dev/null | wc -l | tr -d ' ')
  if [ "$n" != "0" ]; then
    echo "  [保留] 发现 $n 个 .secrets/，默认不删（里面是凭据）。"
    echo "         确认要删请加 --purge-secrets。"
    SKIPPED=$((SKIPPED + n))
  else
    echo "  没有发现 .secrets/"
  fi
elif [ -d "$WORKSPACE" ]; then
  for proj in "$WORKSPACE"/*/; do
    [ -d "$proj" ] || continue
    purge_path "项目 .secrets" "${proj}.secrets"
  done
else
  echo "  工作区不存在: $WORKSPACE"
fi
echo

# ---------------------------------------------------------------------------
# 6. IDE 落点（dec- 前缀，只列顶层条目）
# ---------------------------------------------------------------------------
echo "--- 6. IDE 落点（dec- 前缀）---"
if [ -d "$WORKSPACE" ]; then
  found=0
  for proj in "$WORKSPACE"/*/; do
    for rel in .cursor/skills .claude/skills .codex/skills \
               .cursor/rules .claude/rules .codex/rules; do
      dir="${proj}${rel}"
      [ -d "$dir" ] || continue
      for entry in "$dir"/dec-*; do
        [ -e "$entry" ] || [ -L "$entry" ] || continue
        purge_path "旧落点" "$entry"
        found=1
      done
    done
  done
  [ "$found" -eq 0 ] && echo "  没有发现 dec- 前缀落点"
else
  echo "  工作区不存在: $WORKSPACE"
fi
echo

# ---------------------------------------------------------------------------
# 7. MCP 配置中的 dec-* 条目
# ---------------------------------------------------------------------------
echo "--- 7. MCP 配置中的 dec-* 条目 ---"
purge_mcp_keys() {
  local file="$1"
  [ -f "$file" ] || return 0

  if ! command -v python3 >/dev/null 2>&1; then
    echo "  [跳过] $file : 没有 python3，无法安全改写 JSON"
    return 0
  fi

  python3 - "$file" "$APPLY" <<'PY'
import json, sys

path, apply_flag = sys.argv[1], sys.argv[2] == "1"

try:
    with open(path, "r", encoding="utf-8") as f:
        data = json.load(f)
except Exception as e:
    print(f"  [跳过] {path} : 无法解析 ({e})")
    sys.exit(0)

container_key = None
for k in ("mcpServers", "mcp_servers"):
    if isinstance(data, dict) and isinstance(data.get(k), dict):
        container_key = k
        break

if container_key is None:
    print(f"  [无] {path}")
    sys.exit(0)

servers = data[container_key]
keys = [k for k in servers if k.startswith("dec-")]
if not keys:
    print(f"  [无] {path}")
    sys.exit(0)

if not apply_flag:
    print(f"  [待删] {path} : {', '.join(keys)}")
    sys.exit(0)

for k in keys:
    del servers[k]

with open(path, "w", encoding="utf-8") as f:
    json.dump(data, f, indent=2, ensure_ascii=False)
    f.write("\n")
print(f"  [已删] {path} : {', '.join(keys)}")
PY
}

if [ -d "$WORKSPACE" ]; then
  for proj in "$WORKSPACE"/*/; do
    [ -d "$proj" ] || continue
    for rel in .cursor/mcp.json .claude/mcp.json .mcp.json; do
      purge_mcp_keys "${proj}${rel}"
    done
    toml="${proj}.codex/config.toml"
    if [ -f "$toml" ] && grep -q "^\[mcp_servers\.dec-" "$toml" 2>/dev/null; then
      echo "  [需手工] $toml 含 [mcp_servers.dec-*] 段"
    fi
  done
else
  echo "  工作区不存在: $WORKSPACE"
fi
echo

# ---------------------------------------------------------------------------
# 汇总
# ---------------------------------------------------------------------------
echo "=== 汇总 ==="
echo "  ${MODE}条目: $REMOVED"
echo "  保护/跳过: $SKIPPED"
if [ "$FAILED" -gt 0 ]; then
  echo "  失败: $FAILED" >&2
fi

if [ "$APPLY" -eq 0 ]; then
  echo
  echo "以上为预演，未做任何修改。确认无误后加 --apply 执行。"
else
  echo
  echo "清理完成。保留："
  echo "  ~/.dec-agent/"
  echo "  ~/Library/LaunchAgents/com.dec.agent.plist"
  echo "  ~/.local/bin/dec-agent"
fi

if [ "$FAILED" -gt 0 ]; then exit 1; fi
exit 0
