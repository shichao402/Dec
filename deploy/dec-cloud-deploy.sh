#!/bin/bash
# Dec Cloud 服务器端部署入口（需求 21.2、21.3、21.5、21.6、21.7、21.8）。
#
# 这个脚本是 CI 唯一能执行的东西：部署账户的 authorized_keys 里用
#   command="sudo -n /usr/local/lib/dec-cloud/dec-cloud-deploy.sh",no-port-forwarding,no-agent-forwarding,no-pty
# 把它写死，任何别的命令都进不来（需求 21.6）。因此它不解析任何命令行参数，
# 全部输入来自 stdin 的 tar 流，没有可以注入的位置。
#
# 为什么要 sudo：脚本要写 /opt/dec-cloud（快照、二进制）与 /var/lib/dec-cloud
# （维护标记、部署记录），这些都是 root 或服务账号所有。部署账户本身没有
# 任何其他 sudo 权限，sudoers 只放行这一个脚本路径。
#
# tar 流里必须有：
#
#   args.json     {version, required, commit, tag}
#   dec-cloud     新的服务端二进制
#   release.json  发布清单
#   download/     本次要发布的客户端二进制
#
# 部署顺序固定为（需求 21.2）：
#
#   进入维护 → 暂停冷备 → 快照 → 停服 → 上传 → 启动并迁移 → 健康检查 → 退出维护
#
# 任何一步失败都整体回滚到快照，并写一份部署记录（需求 21.3、21.8）。
#
# 有一条永远不许碰的东西：$DATA_DIR/root.key（需求 21.5）。快照、回滚、
# 上传全都在它之外——脚本里每一次涉及数据的路径都刻意绕开它，并在
# 快照结束时自检一次。

set -euo pipefail

DATA_DIR="${DEC_DATA_DIR:-/var/lib/dec-cloud}"
APP_DIR="${DEC_APP_DIR:-/opt/dec-cloud}"
SNAP_DIR="${DEC_SNAP_DIR:-$APP_DIR/snapshots}"
SERVICE="${DEC_SERVICE:-dec-cloud}"
# 健康检查必须走域名：服务端证书是 ACME 签的，无 SNI 的握手会被拒。
# 域名取 DEC_HEALTH_URL；脚本内部用 --connect-to 把它直连到本机 dec-cloud 的
# 回环监听地址（DEC_LISTEN_ADDR），绕过 nginx，判据只针对 dec-cloud 自身。
HEALTH_URL="${DEC_HEALTH_URL:-https://dec.firoyang.com/healthz}"
# 监听地址不写死：从线上 systemd 单元的 -listen 推导。本脚本不改写单元，
# 单元由 provision.sh 管；若写死 8443，单元还停在旧端口时新版本会被健康检查误判并回滚。
# 推导不到才用默认值。":443" 这类只有端口的写法补成 127.0.0.1。
LISTEN_ADDR="${DEC_LISTEN_ADDR:-}"
if [ -z "$LISTEN_ADDR" ]; then
  LISTEN_ADDR="$(systemctl cat "$SERVICE" 2>/dev/null | sed -n 's/^ExecStart=.*-listen[ =]\([^ ]*\).*/\1/p' | tail -n 1)"
fi
case "$LISTEN_ADDR" in
  "") LISTEN_ADDR="127.0.0.1:8443" ;;
  :*) LISTEN_ADDR="127.0.0.1$LISTEN_ADDR" ;;
  0.0.0.0:*) LISTEN_ADDR="127.0.0.1:${LISTEN_ADDR#0.0.0.0:}" ;;
esac
HEALTH_RETRIES="${DEC_HEALTH_RETRIES:-30}"
HEALTH_INTERVAL="${DEC_HEALTH_INTERVAL:-2}"
# 保留几份快照。够回滚就行，不无限堆积。
KEEP_SNAPSHOTS="${DEC_KEEP_SNAPSHOTS:-2}"

# 部署包里声明的 tag/commit 要向 GitHub 核对真实性（需求 21.7）。
# 仓库与 API 地址都可覆盖，测试时不碰真实 GitHub。
GITHUB_REPO="${DEC_GITHUB_REPO:-shichao402/Dec}"

# 协作目录：服务端轮询它来得知维护状态、冷备暂停与本次部署结果。
DEPLOY_DIR="$DATA_DIR/deploy"

WORK=""
STEP="初始化"
VERSION=""
REQUIRED=""
COMMIT=""
TAG=""
SNAP=""
# RECORDED 表示已经写过部署记录，避免退出收尾时用“异常退出”覆盖掉
# 更具体的回滚原因。
RECORDED=0

log()  { printf '[deploy] %s\n' "$*" >&2; }
fail() { printf '[deploy] 失败（%s）：%s\n' "$STEP" "$*" >&2; }

# write_record 把部署结果写到协作目录，由服务端读走记审计与告警（需求 21.8）。
#
# 写失败不能让部署本身失败：结果已经定了，记不下来只是少一条审计。
write_record() {
  local result="$1" rolled="$2" detail="$3"
  RECORDED=1
  ensure_deploy_dir
  local tmp="$DEPLOY_DIR/pending.json.tmp"
  # detail 里可能有引号和换行，交给 python 转义，避免手拼 JSON 出错。
  RESULT="$result" ROLLED="$rolled" DETAIL="$detail" VERSION="$VERSION" \
  TAG="$TAG" COMMIT="$COMMIT" TMP="$tmp" python3 - <<'PY' 2>/dev/null || return 0
import json, os
payload = {
    "version": os.environ["VERSION"],
    "tag": os.environ["TAG"],
    "commit": os.environ["COMMIT"],
    "result": os.environ["RESULT"],
    "rolled_back": os.environ["ROLLED"] == "true",
    "detail": os.environ["DETAIL"],
}
with open(os.environ["TMP"], "w") as f:
    json.dump(payload, f, ensure_ascii=False)
PY
  chmod 640 "$tmp" 2>/dev/null || true
  mv "$tmp" "$DEPLOY_DIR/pending.json" 2>/dev/null || true
}

set_marker() {
  ensure_deploy_dir
  date -u +%Y-%m-%dT%H:%M:%SZ >"$DEPLOY_DIR/$1"
}
unset_marker() { rm -f "$DEPLOY_DIR/$1"; }

# ensure_deploy_dir 建协作目录，并让它的属主与服务账号一致。
#
# 这个脚本以 root 运行（要写 /opt 与 /var/lib），而服务端以服务账号运行。
# 如果目录被建成 root:root，服务端就没法写维护标记——管理面切换维护状态
# 会静默失败，只剩内存标记，重启即丢。
#
# 服务账号名不写死：从数据目录的属主推导，这样 provision.sh 换了用户名
# 也不会让这里错位。
ensure_deploy_dir() {
  mkdir -p "$DEPLOY_DIR"
  local owner=""
  owner="$(stat -c '%u:%g' "$DATA_DIR" 2>/dev/null || stat -f '%u:%g' "$DATA_DIR" 2>/dev/null || true)"
  if [ -n "$owner" ]; then
    chown "$owner" "$DEPLOY_DIR" 2>/dev/null || true
  fi
  chmod 750 "$DEPLOY_DIR" 2>/dev/null || true
}

# verify_github_release 向 GitHub 核对 tag 真实存在且指向这个 commit（需求 21.7）。
#
# 为什么要问 GitHub：部署凭据是一把只在本机 authorized_keys 里的 SSH key，
# 拿不到 GitHub 的任何权限。因此“这个 tag 在 GitHub 上真的存在并且就是这个
# commit”这个事实，攻击者即使偷到部署私钥也伪造不了——他能做的顶多是重放
# 一个真实存在过的发布。这就是需求 21.7 用等效措施替代 environment 保护规则
# 的全部意义，不能省成只做格式校验。
#
# 仓库是私有的，所以需要一枚只读 token。token 放在 root 专用的 0600 文件里，
# 由 provision.sh 写入。查不到就拒绝部署：宁可发不出去，也不能在无法核实的
# 情况下换二进制。
verify_github_release() {
  local tag="$1" commit="$2" api token_file token resp actual
  api="${DEC_GITHUB_API:-https://api.github.com}"
  token_file="${DEC_GITHUB_TOKEN_FILE:-/etc/dec-cloud/github-token}"
  if [ ! -r "$token_file" ]; then
    fail "读不到 GitHub token（${token_file}），无法核对 tag 真实性"
    return 1
  fi
  token="$(tr -d '\r\n' <"$token_file")"
  if [ -z "$token" ]; then
    fail "GitHub token 为空（${token_file}）"
    return 1
  fi
  # /commits/<ref> 会自动把 tag 解引用到 commit，省掉两步 ref→tag→commit。
  if ! resp="$(curl -fsS --max-time 15 \
      -H "Authorization: Bearer ${token}" \
      -H "Accept: application/vnd.github+json" \
      "${api}/repos/${GITHUB_REPO}/commits/${tag}" 2>/dev/null)"; then
    fail "GitHub 上查不到 tag ${tag}（或网络不可达），拒绝部署"
    return 1
  fi
  actual="$(printf '%s' "$resp" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("sha",""))' 2>/dev/null || true)"
  if [ "$actual" != "$commit" ]; then
    fail "tag ${tag} 在 GitHub 上指向 ${actual:-未知}，与部署包声明的 ${commit} 不一致"
    return 1
  fi
  log "已核对：tag ${tag} 在 GitHub 上确实指向 ${commit:0:8}"
  return 0
}

# wait_health 轮询健康检查，直到服务端报告的版本等于期望版本。
#
# 必须带域名访问，不能直接打 https://127.0.0.1：服务端用 ACME 自动签证书，
# 没有 SNI 的握手会直接失败（acme/autocert: missing server name），
# 表现为「健康检查 30 次全部无响应」然后误回滚——真实踩过。
# --connect-to 保留 URL 里的域名（SNI 正确、拿到正确证书），但 TCP 直连本机
# dec-cloud 的回环端口，不出公网也不经过 nginx。
#
# 用 -k 是刻意的：本机判据是「进程活着、版本对了、能应答」，不是证书链。
wait_health() {
  local want="$1" i body got host
  host="$(printf '%s' "$HEALTH_URL" | sed -e 's|^https\?://||' -e 's|/.*$||' -e 's|:.*$||')"
  for i in $(seq 1 "$HEALTH_RETRIES"); do
    body="$(curl -fsSk --max-time 5 --connect-to "${host}:443:${LISTEN_ADDR}" "$HEALTH_URL" 2>/dev/null || true)"
    if [ -n "$body" ]; then
      got="$(printf '%s' "$body" | tr -d '\n' | sed -n 's/.*"server_version":"\([^"]*\)".*/\1/p')"
      if [ "$got" = "$want" ]; then
        log "健康检查通过：server_version=$got"
        return 0
      fi
      log "健康检查第 $i 次：server_version=${got}（期望 ${want}）"
    else
      log "健康检查第 $i 次：无响应"
    fi
    sleep "$HEALTH_INTERVAL"
  done
  return 1
}

# snapshot 备份当前版本、发布清单、客户端二进制与数据（需求 21.2）。
#
# 排除项：
#   root.key   根密钥，绝不出现在快照里（需求 21.5）
#   deploy/    协作状态，回滚它没有意义
snapshot() {
  local dir="$1"
  mkdir -p "$dir/data"

  if [ -x "$APP_DIR/dec-cloud" ]; then
    cp -p "$APP_DIR/dec-cloud" "$dir/dec-cloud"
  fi
  if [ -f "$DATA_DIR/release.json" ]; then
    cp -p "$DATA_DIR/release.json" "$dir/release.json"
  fi
  if [ -f "$DATA_DIR/required-version" ]; then
    cp -p "$DATA_DIR/required-version" "$dir/required-version"
  fi
  if [ -d "$DATA_DIR/download" ]; then
    cp -a "$DATA_DIR/download" "$dir/download"
  fi

  tar -C "$DATA_DIR" \
    --exclude='./root.key' \
    --exclude='./deploy' \
    -cf - . | tar -C "$dir/data" -xf -

  # 自检：快照里出现根密钥说明 exclude 写错了，这里兜住。
  if [ -e "$dir/data/root.key" ]; then
    rm -f "$dir/data/root.key"
    log "警告：快照里出现根密钥副本，已删除（请检查排除规则）"
  fi
}

# restore 把快照写回去（需求 21.3）。
#
# 数据目录用快照内容覆盖，但不删除快照里没有的文件：
# root.key 本来就不在快照里，覆盖式解包不会碰到它。
restore() {
  local dir="$1"
  log "回滚：恢复服务端二进制、发布清单与数据"

  if [ -f "$dir/dec-cloud" ]; then
    install -m 755 "$dir/dec-cloud" "$APP_DIR/dec-cloud"
  fi
  if [ -f "$dir/release.json" ]; then
    install -m 644 "$dir/release.json" "$DATA_DIR/release.json"
  fi
  if [ -f "$dir/required-version" ]; then
    install -m 644 "$dir/required-version" "$DATA_DIR/required-version"
  fi

  if [ -d "$dir/download" ]; then
    rm -rf "$DATA_DIR/download.rollback"
    cp -a "$dir/download" "$DATA_DIR/download.rollback"
    rm -rf "$DATA_DIR/download"
    mv "$DATA_DIR/download.rollback" "$DATA_DIR/download"
  fi

  if [ -d "$dir/data" ]; then
    tar -C "$dir/data" -cf - . | tar -C "$DATA_DIR" -xf -
  fi
}

# prune_snapshots 只保留最近若干份快照。
#
# 写成显式循环而不是 ls | tail | xargs -r：
# GNU 的 -r 在 BSD xargs 上不存在，而这段脚本在两种系统上都可能被手动跑。
prune_snapshots() {
  local keep="$1" i=0 d
  for d in $(ls -1dt "$SNAP_DIR"/* 2>/dev/null || true); do
    i=$((i + 1))
    if [ "$i" -gt "$keep" ]; then
      rm -rf "$d"
    fi
  done
}

# rollback 回滚并把结果记下来（需求 21.3）。回滚自己也失败时照样留痕。
rollback() {
  local reason="$1" dir="$2"
  # 保留真正失败的步骤名，记录里要写它，而不是“回滚”。
  local failed_step="$STEP"
  STEP="回滚"
  log "回滚：$reason"

  set_marker maintenance
  set_marker pause-backup
  systemctl stop "$SERVICE" 2>/dev/null || true

  if ! restore "$dir"; then
    log "回滚数据失败，服务保持停止状态，需要人工介入"
    write_record "rolled_back" true "失败于 ${failed_step}：${reason}；回滚数据也失败，需人工介入"
    return 1
  fi

  if ! systemctl start "$SERVICE"; then
    log "回滚后启动失败，服务保持停止状态"
    write_record "rolled_back" true "失败于 ${failed_step}：${reason}；回滚后启动失败"
    return 1
  fi

  # 回滚完成后确认旧版本真的回来了，否则“已回滚”只是自我安慰。
  local want=""
  if [ -f "$dir/release.json" ]; then
    want="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["version"])' "$dir/release.json")"
  fi
  if [ -n "$want" ] && ! wait_health "$want"; then
    log "回滚后的健康检查未通过，服务停在维护状态"
    write_record "rolled_back" true "失败于 ${failed_step}：${reason}；回滚后健康检查仍未通过"
    return 1
  fi

  # 回滚成功且旧版本健康：退出维护，让服务恢复可用。
  # 停在维护状态等于用一次失败的部署换来一次无限期停机。
  unset_marker maintenance
  write_record "rolled_back" true "失败于 ${failed_step}：${reason}"
  log "已回滚到 ${want}：${reason}"
  return 0
}

main() {
  STEP="解包"
  WORK="$(mktemp -d)"
  tar -C "$WORK" -xf - || { fail "stdin 不是合法的 tar 流"; return 1; }

  local f
  for f in args.json dec-cloud release.json; do
    if [ ! -f "$WORK/$f" ]; then
      fail "部署包缺少 $f"
      return 1
    fi
  done

  STEP="读取参数"
  VERSION="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["version"])' "$WORK/args.json")"
  REQUIRED="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["required"])' "$WORK/args.json")"
  COMMIT="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["commit"])' "$WORK/args.json")"
  TAG="$(python3 -c 'import json,sys;d=json.load(open(sys.argv[1]));print(d.get("tag") or d["version"])' "$WORK/args.json")"

  # tag 与 commit 形状校验（需求 21.7）。
  #
  # 形状只是第一道：args.json 由 CI 拼出来，拼错了就该在改动任何文件之前失败。
  STEP="校验 tag 与 commit"
  if ! printf '%s' "$TAG" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
    fail "tag 不合法：${TAG}"
    return 1
  fi
  if ! printf '%s' "$COMMIT" | grep -Eq '^[0-9a-f]{40}$'; then
    fail "commit 不合法：${COMMIT}"
    return 1
  fi
  # 版本与 tag 必须是同一个东西：日志、健康检查和回滚比对都靠它。
  if [ "$VERSION" != "$TAG" ]; then
    fail "version ${VERSION} 与 tag ${TAG} 不一致"
    return 1
  fi
  # 真实性：这个 tag 必须在 GitHub 上存在，且指向这个 commit（需求 21.7）。
  if ! verify_github_release "$TAG" "$COMMIT"; then
    return 1
  fi

  # 清单与参数必须一致：CI 组装错了就该在这里失败，
  # 而不是换上一个版本号对不上的二进制（需求 21.7）。
  STEP="校验发布清单"
  local mf_version mf_required
  mf_version="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["version"])' "$WORK/release.json")"
  mf_required="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["required_client"])' "$WORK/release.json")"
  if [ "$mf_version" != "$VERSION" ]; then
    fail "清单版本 ${mf_version} 与参数 ${VERSION} 不一致"
    return 1
  fi
  if [ "$mf_required" != "$REQUIRED" ]; then
    fail "清单要求版本 ${mf_required} 与参数 ${REQUIRED} 不一致"
    return 1
  fi
  log "部署 ${TAG}（commit ${COMMIT:0:8}），要求代理版本 ${REQUIRED}"

  STEP="进入维护"
  set_marker maintenance
  set_marker pause-backup

  STEP="快照"
  SNAP="$SNAP_DIR/$(date -u +%Y%m%dT%H%M%SZ)"
  if ! snapshot "$SNAP"; then
    unset_marker maintenance
    unset_marker pause-backup
    fail "快照失败，未做任何改动"
    return 1
  fi
  log "快照已写入 ${SNAP}（含数据，不含根密钥）"

  STEP="停服"
  systemctl stop "$SERVICE"

  STEP="上传"
  if ! install -m 755 "$WORK/dec-cloud" "$APP_DIR/dec-cloud.new"; then
    rollback "上传服务端二进制失败" "$SNAP"
    return 1
  fi
  rm -rf "$DATA_DIR/download.new"
  mkdir -p "$DATA_DIR/download.new"
  if [ -d "$WORK/download" ]; then
    cp -a "$WORK"/download/. "$DATA_DIR/download.new"/
  fi
  install -m 644 "$WORK/release.json" "$DATA_DIR/release.json.new"
  printf '%s\n' "$REQUIRED" >"$DATA_DIR/required-version.new"

  # 切换：所有 .new 就位后才逐个改名，尽量缩短半成品窗口。
  mv "$APP_DIR/dec-cloud.new" "$APP_DIR/dec-cloud"
  mv "$DATA_DIR/release.json.new" "$DATA_DIR/release.json"
  mv "$DATA_DIR/required-version.new" "$DATA_DIR/required-version"
  rm -rf "$DATA_DIR/download"
  mv "$DATA_DIR/download.new" "$DATA_DIR/download"

  STEP="启动"
  if ! systemctl start "$SERVICE"; then
    rollback "服务启动失败" "$SNAP"
    return 1
  fi

  STEP="健康检查"
  if ! wait_health "$VERSION"; then
    rollback "健康检查未通过" "$SNAP"
    return 1
  fi

  STEP="退出维护"
  unset_marker maintenance
  unset_marker pause-backup
  write_record "ok" false ""

  prune_snapshots "$KEEP_SNAPSHOTS"

  log "部署完成：${TAG}"
  return 0
}

# on_exit 在最外层收尾：清理临时目录，并在非预期退出时补一条记录。
on_exit() {
  local code=$?
  if [ -n "$WORK" ] && [ -d "$WORK" ]; then
    rm -rf "$WORK"
  fi
  if [ "$code" -ne 0 ] && [ "$RECORDED" -eq 0 ]; then
    write_record "rolled_back" true "异常退出于 ${STEP}"
  fi
  # 非零退出时不主动清维护标记：宁可停在维护状态等人来看，
  # 也不要带着一个状态不明的版本对外服务。
}
trap on_exit EXIT

# 生产上这个脚本以 root 运行（要写 /opt、/var/lib 并操作 systemd）。
#
# 这里不硬性要求 uid=0，而是检查目标目录是否可写：判断的是同一件事，
# 但不会把测试沙箱（用 DEC_*_DIR 指到临时目录）也一并挡掉。
# 提前判定而不是让每条命令各自报错，否则会在解包阶段看到一堆
# “Permission denied”，误以为是部署包的问题。
for d in "$APP_DIR" "$DATA_DIR"; do
  parent="$d"
  while [ ! -d "$parent" ]; do
    parent="$(dirname "$parent")"
  done
  if [ ! -w "$parent" ]; then
    fail "无权写入 ${parent}（uid=$(id -u)），需要以 root 执行"
    exit 1
  fi
done
# 判断在 trap 之后，所以这里也要把 RECORDED 置位，避免写出一条无意义的记录。
RECORDED=1

main "$@"
