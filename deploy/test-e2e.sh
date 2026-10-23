#!/usr/bin/env bash
# 在服务器上跑一套完整的线上验收：隔离测试实例 + 真实链路。
#
# 为什么不是直接对生产跑：完整链路要登记设备、写根密钥冷备、写测试 bundle、
# 再删掉。这些动作一旦落在生产上，就是往真实资产库里塞测试数据。
# 所以验收跑在与生产隔离的实例上，生产只做只读检查（scripts/acceptance-prod.sh）。
#
# 隔离靠四件事，缺一条都不算隔离：
#   1. 独立数据目录 /opt/dec-cloud-test/<slot>/data（生产的在 /var/lib/dec-cloud）
#   2. 独立根密钥 /opt/dec-cloud-test/<slot>/secret/root.key（生产的在 /etc/dec-cloud）
#   3. 独立端口，且只监听 127.0.0.1（生产占 443）
#   4. 独立 systemd 单元名 dec-cloud-test / dec-agent-test
#
# 用法（在服务器上以能 sudo 的账号执行）：
#   sudo ./deploy/test-e2e.sh                 # 用默认 slot=test
#   sudo ./deploy/test-e2e.sh --slot demo     # 另开一套，互不干扰
#   sudo ./deploy/test-e2e.sh --reset         # 清掉旧测试数据重来
#
# 前置：需要两个已编译好的 Linux 二进制（由调用方 scp 上来）：
#   /tmp/dec-cloud-test-bin  /tmp/dec-agent-test-bin
set -uo pipefail

SLOT="test"
BASE_DIR="${DEC_TEST_BASE:-/opt/dec-cloud-test}"
SERVER_PORT="${DEC_TEST_PORT:-18443}"
AGENT_PORT="${DEC_TEST_AGENT_PORT:-18787}"
SERVER_BIN="${DEC_TEST_SERVER_BIN:-/tmp/dec-cloud-test-bin}"
AGENT_BIN="${DEC_TEST_AGENT_BIN:-/tmp/dec-agent-test-bin}"
BW_EMAIL="${DEC_TEST_BW_EMAIL:-shichao402.us@gmail.com}"
AGENT_HOME="${DEC_TEST_AGENT_HOME:-/home/decops}"
RUN_AGENT_USER="${DEC_TEST_AGENT_USER:-decops}"

pass=0
fail=0
failed=()

ok() { printf '  [PASS] %s\n' "$1"; pass=$((pass + 1)); }
bad() { printf '  [FAIL] %s\n' "$1"; fail=$((fail + 1)); failed+=("$1"); }
phase() { printf '\n== %s ==\n' "$1"; }
die() { printf '\n失败：%s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
	case "$1" in
		--slot) SLOT="$2"; shift 2 ;;
		--reset) RESET=1; shift ;;
		-h|--help) sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
		*) die "未知参数 $1" ;;
	esac
done

ROOT="$BASE_DIR/$SLOT"
DATA_DIR="$ROOT/data"
KEY_DIR="$ROOT/secret"
AGENT_STATE="$ROOT/agent"

# 写错一个变量就把测试装到生产目录上，这种错误必须动手前拦住。
case "$DATA_DIR" in /var/lib/dec-cloud*) die "数据目录与生产重合：$DATA_DIR" ;; esac
case "$KEY_DIR" in /etc/dec-cloud*) die "根密钥目录与生产重合：$KEY_DIR" ;; esac
[ "$SERVER_PORT" = "443" ] && die "测试实例不能用 443，那是生产的端口"
[ "$SERVER_PORT" = "$AGENT_PORT" ] && die "服务端与代理端口不能相同"

[ -x "$SERVER_BIN" ] || die "找不到服务端二进制 $SERVER_BIN"
[ -x "$AGENT_BIN" ] || die "找不到代理二进制 $AGENT_BIN"

if [ "${RESET:-0}" = "1" ]; then
	phase "清理旧的测试数据"
	systemctl stop "dec-agent-$SLOT" 2>/dev/null
	systemctl stop "dec-cloud-$SLOT" 2>/dev/null
	rm -rf "$ROOT"
	ok "已删除 $ROOT"
fi

mkdir -p "$DATA_DIR" "$KEY_DIR" "$AGENT_STATE"
install -m 755 "$SERVER_BIN" "$ROOT/dec-cloud"
install -m 755 "$AGENT_BIN" "$ROOT/dec-agent"

# 归属是这里最容易错的一处：服务进程以 dec 身份跑，如果数据目录归 root
# 或 decops，进程能启动但**写不进去**——首次初始化会「成功」返回，数据却
# 没落盘，之后每一步都报「设备未登记」。所以目录必须归 dec。
chown -R dec:dec "$DATA_DIR" "$KEY_DIR"
chmod 750 "$DATA_DIR"
chmod 700 "$KEY_DIR"
chown -R "$RUN_AGENT_USER:$RUN_AGENT_USER" "$AGENT_STATE"
chmod 755 "$ROOT"

phase "服务端（只监听 127.0.0.1:$SERVER_PORT）"
cat >/etc/systemd/system/dec-cloud-$SLOT.service <<EOF
[Unit]
Description=Dec Cloud (isolated test slot: $SLOT)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=dec
Group=dec
NoNewPrivileges=yes
ExecStart=$ROOT/dec-cloud -data-dir $DATA_DIR -listen 127.0.0.1:$SERVER_PORT -domain localhost -accept-any -root-key $KEY_DIR/root.key
Restart=on-failure
RestartSec=5s
TimeoutStopSec=15s
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=$DATA_DIR $KEY_DIR

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "dec-cloud-$SLOT" >/dev/null 2>&1 || true
systemctl restart "dec-cloud-$SLOT"
sleep 3

systemctl is-active --quiet "dec-cloud-$SLOT" && ok "服务端已启动" || {
	journalctl -u "dec-cloud-$SLOT" --no-pager -n 15
	die "服务端没有起来"
}

listen=$(ss -lnt 2>/dev/null | awk -v p=":$SERVER_PORT" '$4 ~ p {print $4}' | head -1)
case "$listen" in
	127.0.0.1:*|"[::1]:"*) ok "只监听回环：$listen" ;;
	*) systemctl stop "dec-cloud-$SLOT"; die "监听地址不是回环：${listen:-无}" ;;
esac

curl -skS --max-time 10 "https://127.0.0.1:$SERVER_PORT/healthz" | grep -q '"ok":true' &&
	ok "healthz 健康" || die "healthz 不健康"

phase "代理初始化"
if [ -f "$KEY_DIR/root.key" ] && [ -s "$AGENT_STATE/identity/identity.json" ]; then
	ok "已有根密钥与设备身份，跳过初始化"
else
	# 先写服务地址：initialize 会读它，没配就直接拒绝。
	# 这一步放在密码之前，缺配置时不必先走一遍 Bitwarden 登录。
	su -s /bin/bash "$RUN_AGENT_USER" -c \
		"DEC_AGENT_STATE='$AGENT_STATE' '$ROOT/dec-agent' configure --url https://127.0.0.1:$SERVER_PORT" >/dev/null ||
		die "配置服务地址失败"
	ok "服务地址已配置：https://127.0.0.1:$SERVER_PORT"

	# 密码经 stdin 传入，不出现在命令行里（ps 对同机所有用户可见）。
	printf '%s' "${DEC_BW_PASSWORD:-}" | su -s /bin/bash "$RUN_AGENT_USER" -c \
		"DEC_AGENT_STATE='$AGENT_STATE' DEC_BW_PASSWORD=\"\$(cat)\" '$ROOT/dec-agent' initialize --email '$BW_EMAIL' --name 'test-$SLOT' --insecure" ||
		die "初始化失败（需要 DEC_BW_PASSWORD，测试账号见 .secrets/dec/integration/bitwarden.yaml）"
	ok "初始化完成（根密钥已写入测试账号的 Bitwarden 冷备）"
fi

phase "代理（127.0.0.1:$AGENT_PORT）"
cat >/etc/systemd/system/dec-agent-$SLOT.service <<EOF
[Unit]
Description=Dec Agent (isolated test slot: $SLOT)
After=network-online.target dec-cloud-$SLOT.service
Wants=network-online.target

[Service]
Type=simple
User=$RUN_AGENT_USER
Group=$RUN_AGENT_USER
Environment=DEC_AGENT_STATE=$AGENT_STATE
Environment=HOME=$AGENT_HOME
ExecStart=$ROOT/dec-agent run --insecure --port $AGENT_PORT --name test-$SLOT
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "dec-agent-$SLOT" >/dev/null 2>&1 || true
systemctl restart "dec-agent-$SLOT"
sleep 5

systemctl is-active --quiet "dec-agent-$SLOT" && ok "代理已启动" || {
	journalctl -u "dec-agent-$SLOT" --no-pager -n 15
	die "代理没有起来"
}

status=$(curl -sS --max-time 10 "http://127.0.0.1:$AGENT_PORT/api/status" 2>/dev/null)
printf '%s' "$status" | grep -q '"aligned":true' && ok "代理已对齐（aligned）" || {
	printf '  状态：%s\n' "$status"
	die "代理未对齐，看 last_error"
}

phase "真实链路验收"
# --skip-hosts：服务器上没有 IDE，写不出 MCP 条目，这不是缺陷。
# 在个人电脑上跑时必须省略这个参数。
su -s /bin/bash "$RUN_AGENT_USER" -c \
	"DEC_AGENT_STATE='$AGENT_STATE' HOME='$AGENT_HOME' '$ROOT/dec-agent' acceptance --url http://127.0.0.1:$AGENT_PORT --skip-hosts"
ACCEPT=$?

phase "残留与隔离核对"
leftovers=$(ls "$DATA_DIR/ssot/assets" 2>/dev/null | grep '^dec-acceptance' | tr '\n' ' ')
[ -z "$leftovers" ] && ok "测试实例没有残留 bundle" || bad "残留 bundle：$leftovers"

# 生产库必须看不到测试写入的任何东西。这条不成立就说明隔离漏了。
prod_assets=$(ls /var/lib/dec-cloud/ssot/assets 2>/dev/null | grep '^dec-acceptance' | tr '\n' ' ')
[ -z "$prod_assets" ] && ok "生产库没有测试残留" || bad "生产库出现测试 bundle：$prod_assets"

systemctl is-active --quiet dec-cloud && ok "生产服务仍在运行" || bad "生产服务异常"

printf '\n结果：通过 %d 项，失败 %d 项\n' "$pass" "$fail"
if [ "$ACCEPT" -ne 0 ]; then
	printf '链路验收未通过（退出码 %s）\n' "$ACCEPT"
	exit 1
fi
[ "$fail" -gt 0 ] && { printf '未通过：%s\n' "${failed[*]}"; exit 1; }
echo "线上验收通过：隔离实例跑完完整链路，生产未受影响。"
