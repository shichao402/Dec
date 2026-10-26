#!/bin/bash
# 部署入口脚本的端到端测试。
#
# 这个脚本跑在 CI 与服务器上，出错代价是“服务换了版本但没起来”。
# 所以用一个假 systemctl + 假健康检查服务把它整条链路跑通，
# 覆盖：正常部署、健康检查失败回滚、启动失败回滚、根密钥不受影响、
# 以及不合法的部署包被拒绝。
#
# 用法：deploy/test.sh
#
# 它不是 go test 的一部分：这里测的是 shell 与文件交互，
# 用 Go 写反而要再造一套假 systemd。

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/dec-cloud-deploy.sh"
FAILED=0
PASSED=0

ok()   { PASSED=$((PASSED + 1)); printf '  ok   %s\n' "$*"; }
bad()  { FAILED=$((FAILED + 1)); printf '  FAIL %s\n' "$*"; }
check() { if eval "$2"; then ok "$1"; else bad "$1"; fi }

# newenv 建一个独立的沙箱：假 systemctl、假 curl、独立的数据与程序目录。
newenv() {
  ENV_DIR="$(mktemp -d)"
  export PATH="$ENV_DIR/bin:$PATH"
  mkdir -p "$ENV_DIR/bin" "$ENV_DIR/app" "$ENV_DIR/data" "$ENV_DIR/snap"

  export DEC_APP_DIR="$ENV_DIR/app"
  export DEC_DATA_DIR="$ENV_DIR/data"
  export DEC_SNAP_DIR="$ENV_DIR/snap"
  # 断言里用简写，避免每条 check 都写 DEC_ 前缀。
  export APP_DIR="$DEC_APP_DIR"
  export DATA_DIR="$DEC_DATA_DIR"
  export SNAP_DIR="$DEC_SNAP_DIR"
  export DEC_SERVICE="dec-cloud-test"
  export DEC_HEALTH_URL="http://127.0.0.1:1/healthz"
  export DEC_HEALTH_RETRIES="2"
  export DEC_HEALTH_INTERVAL="0"

  # 假 curl：按 URL 分流。
  #   api.github.test  → GitHub 真实性核对，返回 $FAKE_GH_SHA（未设置为 404）
  #   其它（healthz）  → 健康检查，返回 $FAKE_HEALTH_BODY
  cat >"$ENV_DIR/bin/curl" <<'SH'
#!/bin/bash
url=""
for a in "$@"; do
  case "$a" in
    http://*|https://*) url="$a" ;;
  esac
done
case "$url" in
  *api.github.test*)
    if [ -z "${FAKE_GH_SHA:-}" ]; then
      exit 22
    fi
    printf '{"sha":"%s"}' "$FAKE_GH_SHA"
    exit 0
    ;;
esac
if [ -n "${FAKE_HEALTH_BODY:-}" ]; then
  printf '%s' "$FAKE_HEALTH_BODY"
  exit 0
fi
exit 22
SH
  chmod +x "$ENV_DIR/bin/curl"

  # 假 systemctl：记录调用；start 可以按 $FAKE_START_FAIL 失败。
  cat >"$ENV_DIR/bin/systemctl" <<'SH'
#!/bin/bash
printf '%s\n' "$*" >>"$FAKE_SYSTEMCTL_LOG"
if [ "$1" = "start" ] && [ "${FAKE_START_FAIL:-}" = "1" ]; then
  exit 1
fi
exit 0
SH
  chmod +x "$ENV_DIR/bin/systemctl"

  export FAKE_SYSTEMCTL_LOG="$ENV_DIR/systemctl.log"
  : >"$FAKE_SYSTEMCTL_LOG"

  # 真实性核对要打 GitHub API。测试里把 API 指向假地址，由下面的假 curl
  # 按 URL 分流：healthz 走 $FAKE_HEALTH_BODY，GitHub 走 $FAKE_GH_SHA。
  export DEC_GITHUB_API="https://api.github.test"
  export DEC_GITHUB_REPO="owner/repo"
  export DEC_GITHUB_TOKEN_FILE="$ENV_DIR/gh-token"
  printf 'fake-token\n' >"$DEC_GITHUB_TOKEN_FILE"
  chmod 600 "$DEC_GITHUB_TOKEN_FILE"

  # 现有安装：旧版本二进制、发布清单与根密钥。
  printf 'old-binary\n' >"$ENV_DIR/app/dec-cloud"
  chmod 755 "$ENV_DIR/app/dec-cloud"
  printf 'old-emergency\n' >"$ENV_DIR/app/dec-emergency"
  chmod 755 "$ENV_DIR/app/dec-emergency"
  printf '{"version":"v1.0.0","required_client":"v1.0.0","protocol":1,"files":[]}\n' \
    >"$ENV_DIR/data/release.json"
  printf 'v1.0.0\n' >"$ENV_DIR/data/required-version"
  mkdir -p "$ENV_DIR/data/download"
  printf 'old-agent\n' >"$ENV_DIR/data/download/dec-agent-linux-amd64"
  printf 'ROOTKEY-SECRET\n' >"$ENV_DIR/data/root.key"
  chmod 600 "$ENV_DIR/data/root.key"
  mkdir -p "$ENV_DIR/data/ssot" "$ENV_DIR/data/state"
  printf 'ssot\n' >"$ENV_DIR/data/ssot/HEAD"
}

# mkpayload 生成一个部署包，写到 $2。
mkpayload() {
  local version="$1" out="$2" required="${3:-$1}"
  local stage
  stage="$(mktemp -d)"
  printf 'new-binary-%s\n' "$version" >"$stage/dec-cloud"
  chmod 755 "$stage/dec-cloud"
  printf 'new-emergency-%s\n' "$version" >"$stage/dec-emergency"
  chmod 755 "$stage/dec-emergency"
  printf '{"version":"%s","required_client":"%s","protocol":1,"files":[]}\n' \
    "$version" "$required" >"$stage/release.json"
  mkdir -p "$stage/download"
  printf 'new-agent-%s\n' "$version" >"$stage/download/dec-agent-linux-amd64"
  python3 - "$version" "$required" "$out" >"$stage/args.json" <<'PY'
import json, sys
version, required, _ = sys.argv[1:4]
json.dump({"version": version, "required": required,
           "commit": "0123456789abcdef0123456789abcdef01234567",
           "tag": version}, sys.stdout, ensure_ascii=False)
PY
  tar -C "$stage" -cf "$out" .
  rm -rf "$stage"
  # 默认让 GitHub 侧核对的 sha 与包里声明的一致，正常路径才能通过。
  export FAKE_GH_SHA="0123456789abcdef0123456789abcdef01234567"
}

cleanup_env() {
  rm -rf "$ENV_DIR"
  unset DEC_APP_DIR DEC_DATA_DIR DEC_SNAP_DIR DEC_SERVICE DEC_HEALTH_URL
  unset DEC_HEALTH_RETRIES DEC_HEALTH_INTERVAL FAKE_HEALTH_BODY FAKE_START_FAIL
}

echo "部署脚本测试"

# 1. 正常部署：新版本上线、维护标记清掉、部署记录写成功。
newenv
mkpayload "v1.1.0" "$ENV_DIR/payload.tar"
export FAKE_HEALTH_BODY='{"ok":false,"server_version":"v1.1.0","required_client":"v1.1.0","protocol":1,"maintenance":true}'
if "$SCRIPT" <"$ENV_DIR/payload.tar" >/dev/null 2>&1; then
  ok "正常部署应成功"
else
  bad "正常部署应成功"
fi
check "服务端二进制已替换" '[ "$(cat "$DEC_APP_DIR/dec-cloud")" = "new-binary-v1.1.0" ]'
check "应急命令已替换" '[ "$(cat "$DEC_APP_DIR/dec-emergency")" = "new-emergency-v1.1.0" ]'
check "发布清单已更新" 'grep -q v1.1.0 "$DEC_DATA_DIR/release.json"'
check "要求版本已写入" '[ "$(cat "$DEC_DATA_DIR/required-version")" = "v1.1.0" ]'
check "客户端二进制已更新" '[ "$(cat "$DEC_DATA_DIR/download/dec-agent-linux-amd64")" = "new-agent-v1.1.0" ]'
check "维护标记已清除" '[ ! -e "$DEC_DATA_DIR/deploy/maintenance" ]'
check "冷备暂停标记已清除" '[ ! -e "$DEC_DATA_DIR/deploy/pause-backup" ]'
check "部署记录结果为 ok" 'grep -q "\"result\": \"ok\"" "$DEC_DATA_DIR/deploy/pending.json"'
check "根密钥未被改动" '[ "$(cat "$DEC_DATA_DIR/root.key")" = "ROOTKEY-SECRET" ]'
check "快照里没有根密钥" '[ -z "$(find "$DEC_SNAP_DIR" -name root.key -print -quit)" ]'
check "快照存在且含二进制" '[ -n "$(find "$DEC_SNAP_DIR" -name dec-cloud -print -quit)" ]'
cleanup_env

# 2. 健康检查不通过：回滚到旧版本，维护标记保留，记录为回滚。
newenv
mkpayload "v1.2.0" "$ENV_DIR/payload.tar"
export FAKE_HEALTH_BODY='{"ok":false,"server_version":"v1.0.0","required_client":"v1.0.0","protocol":1,"maintenance":true}'
if "$SCRIPT" <"$ENV_DIR/payload.tar" >/dev/null 2>&1; then
  bad "健康检查失败时脚本应返回非零"
else
  ok "健康检查失败时脚本应返回非零"
fi
check "已回滚到旧二进制" '[ "$(cat "$DEC_APP_DIR/dec-cloud")" = "old-binary" ]'
check "已回滚应急命令" '[ "$(cat "$DEC_APP_DIR/dec-emergency")" = "old-emergency" ]'
check "已回滚发布清单" 'grep -q v1.0.0 "$DEC_DATA_DIR/release.json"'
# 旧版本健健康检查通过时应该退出维护：停在维护等于用一次失败部署换来一次停机。
check "回滚成功后已退出维护" '[ ! -e "$DEC_DATA_DIR/deploy/maintenance" ]'
check "部署记录为 rolled_back" 'grep -q "\"result\": \"rolled_back\"" "$DATA_DIR/deploy/pending.json"'
check "记录里写明失败步骤" 'grep -q "健康检查未通过" "$DATA_DIR/deploy/pending.json"'
check "根密钥未被改动" '[ "$(cat "$DEC_DATA_DIR/root.key")" = "ROOTKEY-SECRET" ]'
cleanup_env

# 2b. 回滚后旧版本也不健康：必须停在维护状态等人处理。
newenv
mkpayload "v1.2.1" "$ENV_DIR/payload.tar"
export FAKE_HEALTH_BODY='{"ok":false,"server_version":"v0.0.0","required_client":"v0.0.0","protocol":1,"maintenance":true}'
if "$SCRIPT" <"$ENV_DIR/payload.tar" >/dev/null 2>&1; then
  bad "回滚后仍不健康时应返回非零"
else
  ok "回滚后仍不健康时应返回非零"
fi
check "回滚后仍不健康时保持维护" '[ -e "$DEC_DATA_DIR/deploy/maintenance" ]'
check "记录里说明回滚后仍不健康" 'grep -q "回滚后健康检查仍未通过" "$DATA_DIR/deploy/pending.json"'
cleanup_env

# 3. 启动失败：同样回滚。
newenv
mkpayload "v1.3.0" "$ENV_DIR/payload.tar"
export FAKE_START_FAIL=1
if "$SCRIPT" <"$ENV_DIR/payload.tar" >/dev/null 2>&1; then
  bad "启动失败时脚本应返回非零"
else
  ok "启动失败时脚本应返回非零"
fi
check "启动失败后回滚到旧二进制" '[ "$(cat "$DEC_APP_DIR/dec-cloud")" = "old-binary" ]'
check "启动失败后仍处于维护" '[ -e "$DEC_DATA_DIR/deploy/maintenance" ]'
cleanup_env

# 4. 清单与参数不一致：必须在动手之前拒绝。
newenv
mkpayload "v1.4.0" "$ENV_DIR/payload.tar"
# 把清单里的版本改成别的，模拟 CI 组装错误。
python3 - "$ENV_DIR" <<'PY'
import json, os, shutil, subprocess, sys, tarfile
env = sys.argv[1]
stage = os.path.join(env, "bad")
os.makedirs(stage, exist_ok=True)
with tarfile.open(os.path.join(env, "payload.tar")) as tf:
    tf.extractall(stage)
mf = os.path.join(stage, "release.json")
data = json.load(open(mf))
data["version"] = "v9.9.9"
json.dump(data, open(mf, "w"))
subprocess.check_call(["tar", "-C", stage, "-cf", os.path.join(env, "bad.tar"), "."])
PY
if "$SCRIPT" <"$ENV_DIR/bad.tar" >/dev/null 2>&1; then
  bad "清单不一致时脚本应拒绝"
else
  ok "清单不一致时脚本应拒绝"
fi
check "拒绝时未改动二进制" '[ "$(cat "$DEC_APP_DIR/dec-cloud")" = "old-binary" ]'
check "拒绝时未改动清单" 'grep -q v1.0.0 "$DEC_DATA_DIR/release.json"'
check "拒绝时未碰数据" '[ "$(cat "$DEC_DATA_DIR/ssot/HEAD")" = "ssot" ]'
cleanup_env

# 5. 缺少必需文件：拒绝且不动任何东西。
newenv
stage="$(mktemp -d)"
printf 'x\n' >"$stage/dec-cloud"
tar -C "$stage" -cf "$ENV_DIR/missing.tar" .
if "$SCRIPT" <"$ENV_DIR/missing.tar" >/dev/null 2>&1; then
  bad "缺少 args.json 时应拒绝"
else
  ok "缺少 args.json 时应拒绝"
fi
check "拒绝时未改二进制" '[ "$(cat "$DEC_APP_DIR/dec-cloud")" = "old-binary" ]'
rm -rf "$stage"
cleanup_env

# 6. 非法 stdin：拒绝。
newenv
if printf 'not a tar' | "$SCRIPT" >/dev/null 2>&1; then
  bad "非法 tar 流应拒绝"
else
  ok "非法 tar 流应拒绝"
fi
check "非法输入未改二进制" '[ "$(cat "$DEC_APP_DIR/dec-cloud")" = "old-binary" ]'
cleanup_env

# 6b. tag / commit 形状不合法：必须在动手之前拒绝（需求 21.7）。
badpayload() {
  local name="$1" tag="$2" commit="$3" version="$4"
  local stage
  stage="$(mktemp -d)"
  printf 'bad\n' >"$stage/dec-cloud"
  chmod 755 "$stage/dec-cloud"
  printf '{"version":"%s","required_client":"%s","protocol":1,"files":[]}\n' \
    "$version" "$version" >"$stage/release.json"
  python3 - "$version" "$tag" "$commit" "$ENV_DIR/$name" >"$stage/args.json" <<'PY'
import json, sys
version, tag, commit, _ = sys.argv[1:5]
json.dump({"version": version, "required": version,
           "commit": commit, "tag": tag}, sys.stdout, ensure_ascii=False)
PY
  tar -C "$stage" -cf "$ENV_DIR/$name" .
  rm -rf "$stage"
}

newenv
badpayload "badtag.tar" "1.9.0" "0123456789abcdef0123456789abcdef01234567" "1.9.0"
if "$SCRIPT" <"$ENV_DIR/badtag.tar" >/dev/null 2>&1; then
  bad "非法 tag 应拒绝"
else
  ok "非法 tag 应拒绝"
fi
check "拒绝非法 tag 时未改二进制" '[ "$(cat "$DEC_APP_DIR/dec-cloud")" = "old-binary" ]'
check "拒绝非法 tag 时未进维护" '[ ! -e "$DEC_DATA_DIR/deploy/maintenance" ]'
cleanup_env

newenv
badpayload "badcommit.tar" "v1.9.1" "HEAD" "v1.9.1"
if "$SCRIPT" <"$ENV_DIR/badcommit.tar" >/dev/null 2>&1; then
  bad "非法 commit 应拒绝"
else
  ok "非法 commit 应拒绝"
fi
check "拒绝非法 commit 时未进维护" '[ ! -e "$DEC_DATA_DIR/deploy/maintenance" ]'
cleanup_env

newenv
badpayload "mismatch.tar" "v1.9.2" "0123456789abcdef0123456789abcdef01234567" "v1.9.3"
if "$SCRIPT" <"$ENV_DIR/mismatch.tar" >/dev/null 2>&1; then
  bad "version 与 tag 不一致应拒绝"
else
  ok "version 与 tag 不一致应拒绝"
fi
check "拒绝版本错位时未改二进制" '[ "$(cat "$DEC_APP_DIR/dec-cloud")" = "old-binary" ]'
cleanup_env

# 6c. GitHub 上没有这个 tag：拒绝（需求 21.7）。
#     偷到部署私钥的人可以伪造任意 args.json，但只要这个 tag 在 GitHub 上
#     不存在，部署就必须在这里停下。
newenv
mkpayload "v1.9.4" "$ENV_DIR/payload.tar"
unset FAKE_GH_SHA
export FAKE_HEALTH_BODY='{"ok":false,"server_version":"v1.9.4","required_client":"v1.9.4","protocol":1,"maintenance":true}'
if "$SCRIPT" <"$ENV_DIR/payload.tar" >/dev/null 2>&1; then
  bad "GitHub 上不存在的 tag 应拒绝"
else
  ok "GitHub 上不存在的 tag 应拒绝"
fi
check "拒绝未知 tag 时未改二进制" '[ "$(cat "$DEC_APP_DIR/dec-cloud")" = "old-binary" ]'
check "拒绝未知 tag 时未进维护" '[ ! -e "$DEC_DATA_DIR/deploy/maintenance" ]'
check "拒绝未知 tag 时未写部署记录" '[ ! -e "$DEC_DATA_DIR/deploy/pending.json" ] || ! grep -q "\"result\": \"ok\"" "$DEC_DATA_DIR/deploy/pending.json"'
cleanup_env

# 6d. tag 存在但指向别的 commit：拒绝。
newenv
mkpayload "v1.9.5" "$ENV_DIR/payload.tar"
export FAKE_GH_SHA="ffffffffffffffffffffffffffffffffffffffff"
export FAKE_HEALTH_BODY='{"ok":false,"server_version":"v1.9.5","required_client":"v1.9.5","protocol":1,"maintenance":true}'
if "$SCRIPT" <"$ENV_DIR/payload.tar" >/dev/null 2>&1; then
  bad "commit 与 tag 不一致应拒绝"
else
  ok "commit 与 tag 不一致应拒绝"
fi
check "拒绝 commit 错位时未改二进制" '[ "$(cat "$DEC_APP_DIR/dec-cloud")" = "old-binary" ]'
cleanup_env

# 6e. 读不到 GitHub token：拒绝。少了 token 就无法核实，不能放行。
newenv
mkpayload "v1.9.6" "$ENV_DIR/payload.tar"
rm -f "$DEC_GITHUB_TOKEN_FILE"
export FAKE_HEALTH_BODY='{"ok":false,"server_version":"v1.9.6","required_client":"v1.9.6","protocol":1,"maintenance":true}'
if "$SCRIPT" <"$ENV_DIR/payload.tar" >/dev/null 2>&1; then
  bad "缺少 GitHub token 应拒绝"
else
  ok "缺少 GitHub token 应拒绝"
fi
check "拒绝缺 token 时未改二进制" '[ "$(cat "$DEC_APP_DIR/dec-cloud")" = "old-binary" ]'
cleanup_env

# 7. 只保留最近两份快照。
newenv
for v in v1.5.0 v1.5.1 v1.5.2; do
  mkpayload "$v" "$ENV_DIR/payload.tar"
  export FAKE_HEALTH_BODY="{\"ok\":false,\"server_version\":\"$v\",\"required_client\":\"$v\",\"protocol\":1,\"maintenance\":true}"
  "$SCRIPT" <"$ENV_DIR/payload.tar" >/dev/null 2>&1 || true
  # 时间戳精确到秒，连续调用会同名；睡一下让目录名区分开。
  sleep 1
done
check "快照只保留最近两份" '[ "$(ls -1d "$DEC_SNAP_DIR"/* 2>/dev/null | wc -l | tr -d " ")" -le 2 ]'
cleanup_env

echo
printf '通过 %d，失败 %d\n' "$PASSED" "$FAILED"
[ "$FAILED" -eq 0 ]
