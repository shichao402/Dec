#!/bin/bash
# Dec Cloud 服务器声明式配置（需求 16.1、16.2、16.3a、16.5、21.6）。
#
# 一条命令从零重建服务器：账号、目录、权限、SSH 加固、fail2ban、
# 自动安全更新、systemd 单元、部署账户的受限入口。
#
# 用法（在服务器上以 root 执行）：
#
#   ./provision.sh --domain dec.firoyang.com \
#                  --deploy-key "ssh-ed25519 AAAA... ci@github" \
#                  --ops-key    "ssh-ed25519 AAAA... me@laptop"
#
# 幂等：重复执行只是把状态收敛到声明，不会重复创建或破坏已有数据。
#
# 刻意不做的事：
#
#   - 不写 root.key。根密钥只能由服务在初始化流程里生成（需求 9.1）；
#     任何脚本预先放置密钥都会绕过 Bitwarden 冷备的写入与读回校验。
#   - 不申请证书。证书由服务自己用 TLS-ALPN-01 取得（需求 7）。
#   - 不配置任何 Dec 业务数据。资产与密钥一律走协议写入链路。

set -euo pipefail

DOMAIN=""
DEPLOY_KEY=""
OPS_KEY=""
OPS_USER="${DEC_OPS_USER:-decops}"
DEPLOY_USER="${DEC_DEPLOY_USER:-deploy}"
SERVICE_USER="${DEC_SERVICE_USER:-dec}"
SERVICE_NAME="dec-cloud"
# 根密钥目录单独放在 /etc 下：数据目录（/var/lib/dec-cloud）会被整体快照，
# /etc 不在里面，因此根密钥天然不进快照（需求 16.5、21.5）。
ROOT_KEY_DIR="${DEC_ROOT_KEY_DIR:-/etc/dec-cloud}"
DATA_DIR="/var/lib/dec-cloud"
APP_DIR="/opt/dec-cloud"
# 部署入口：authorized_keys 的 command= 与 sudoers 都指向这一个路径，
# 只改一处就会两边不一致，所以集中定义。
ENTRY_PATH="/usr/local/lib/${SERVICE_NAME}/${SERVICE_NAME}-deploy.sh"

log()  { printf '[provision] %s\n' "$*"; }
die()  { printf '[provision] 失败：%s\n' "$*" >&2; exit 1; }

usage() {
  sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//'
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --domain)     DOMAIN="${2:-}"; shift 2 ;;
    --deploy-key) DEPLOY_KEY="${2:-}"; shift 2 ;;
    --ops-key)    OPS_KEY="${2:-}"; shift 2 ;;
    -h|--help)    usage ;;
    *)            die "未知参数 $1" ;;
  esac
done

[ "$(id -u)" -eq 0 ] || die "需要 root 执行"
[ -n "$DOMAIN" ] || die "必须给 --domain"
# 至少要有一个人能登录，否则配完之后只能走控制台。
[ -n "$DEPLOY_KEY" ] || [ -n "$OPS_KEY" ] || die "--deploy-key 与 --ops-key 至少给一个"

# --- 账号 ---------------------------------------------------------------
#
# 三个账号，职责互不重叠（需求 16.3a、21.6）：
#   $SERVICE_USER  跑服务，无 shell，无 sudo
#   $DEPLOY_USER   CI 专用，authorized_keys 用 command= 锁死到一个脚本
#   $OPS_USER      用户本人运维，唯一有 sudo 的普通账号

ensure_user() {
  local name="$1" shell="$2" home="$3"
  if ! id -u "$name" >/dev/null 2>&1; then
    useradd --create-home --home-dir "$home" --shell "$shell" "$name"
    log "已创建账号 $name"
  fi
}

ensure_service_user() {
  local name="$1" home="$2"
  if ! id -u "$name" >/dev/null 2>&1; then
    useradd --system --create-home --home-dir "$home" --shell /usr/sbin/nologin "$name"
    log "已创建服务账号 $name"
  fi
}

ensure_service_user "$SERVICE_USER" "$DATA_DIR"
# 部署与运维账户要能登录，因此给真实 shell；权限靠 sudoers 与 command= 收紧。
ensure_user "$DEPLOY_USER" /bin/bash "/home/$DEPLOY_USER"
ensure_user "$OPS_USER" /bin/bash "/home/$OPS_USER"

# --- 目录与权限 ---------------------------------------------------------

install -d -m 755 -o root -g root "$APP_DIR"
install -d -m 755 -o root -g root "$APP_DIR/snapshots"
# 数据目录归属服务账号；部署账户只通过部署脚本间接写入。
install -d -m 750 -o "$SERVICE_USER" -g "$SERVICE_USER" "$DATA_DIR"
# 根密钥目录：root 所有、服务账号可读，别的账号进不来。
install -d -m 750 -o root -g "$SERVICE_USER" "$ROOT_KEY_DIR"
# 部署脚本要向 GitHub 核对 tag 真实性（需求 21.7），token 只给 root 读。
# 这里只建空占位并锁权限，值由人填——把 token 写进脚本或仓库都等于泄露。
if [ ! -f "$ROOT_KEY_DIR/github-token" ]; then
  install -m 600 -o root -g root /dev/null "$ROOT_KEY_DIR/github-token"
  log "已创建 $ROOT_KEY_DIR/github-token 占位，需填入只读 token（见部署文档）"
fi
# 部署脚本放在只有 root 可写的路径：部署账户能执行但不能改。
install -d -m 755 -o root -g root /usr/local/lib/dec-cloud

# --- authorized_keys ----------------------------------------------------
#
# 先写 key，再改 SSH 配置。顺序很重要：一旦 PermitRootLogin no 生效，
# 只有这两个账号能进来；如果它们还没有 authorized_keys，就等于把门锁死
# 而钥匙还没配好。已经建立的会话不受影响，但下一次连接就进不来了。
#
# 部署账户的 key 用 command= 锁死到部署入口（需求 21.6）：
# 即使私钥泄漏，攻击者也只能触发一次部署，拿不到 shell。

deploy_home="/home/$DEPLOY_USER"
install -d -m 700 -o "$DEPLOY_USER" -g "$DEPLOY_USER" "$deploy_home/.ssh"
if [ -n "$DEPLOY_KEY" ]; then
  {
    printf 'command="sudo -n %s",no-pty,no-port-forwarding,' "$ENTRY_PATH"
    printf 'no-agent-forwarding,no-X11-forwarding,restrict %s\n' "$DEPLOY_KEY"
  } >"$deploy_home/.ssh/authorized_keys"
  chown "$DEPLOY_USER:$DEPLOY_USER" "$deploy_home/.ssh/authorized_keys"
  chmod 600 "$deploy_home/.ssh/authorized_keys"
  log "部署账户 $DEPLOY_USER 已限定为只能执行部署入口"
fi

if [ -n "$OPS_KEY" ]; then
  ops_home="/home/$OPS_USER"
  install -d -m 700 -o "$OPS_USER" -g "$OPS_USER" "$ops_home/.ssh"
  printf '%s\n' "$OPS_KEY" >"$ops_home/.ssh/authorized_keys"
  chown "$OPS_USER:$OPS_USER" "$ops_home/.ssh/authorized_keys"
  chmod 600 "$ops_home/.ssh/authorized_keys"
  log "运维账户 $OPS_USER 已配置公钥"
fi

# --- SSH 加固（需求 16.3a、21.6）----------------------------------------

sshd_dropin=/etc/ssh/sshd_config.d/99-dec-cloud.conf
mkdir -p /etc/ssh/sshd_config.d
cat >"$sshd_dropin" <<EOF
# Dec Cloud 的 SSH 基线。
# 只允许公钥登录，禁止密码与 root 直接登录，并限制可登录的账号。
PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
AllowUsers $DEPLOY_USER $OPS_USER
X11Forwarding no
AllowAgentForwarding no
AllowTcpForwarding no
MaxAuthTries 3
EOF

# 先验证配置再 reload：写坏了 sshd 配置比不加固更危险。
if ! sshd -t; then
  rm -f "$sshd_dropin"
  die "sshd 配置校验失败，已移除本次写入的配置，未 reload"
fi
# 重载后确认 sshd 还活着。这条检查是给自己留退路：配置语法通过但语义
# 有问题（比如把当前登录的账号漏在 AllowUsers 外）时，reload 之后新连接
# 会被拒，已经建立的会话还在，但下一次就进不来了。
systemctl reload sshd 2>/dev/null || systemctl reload ssh 2>/dev/null || true
if ! systemctl is-active --quiet sshd 2>/dev/null && ! systemctl is-active --quiet ssh 2>/dev/null; then
  rm -f "$sshd_dropin"
  systemctl restart sshd 2>/dev/null || systemctl restart ssh 2>/dev/null || true
  die "sshd 重载后未在运行，已移除本次配置并尝试恢复"
fi
log "SSH 已加固：仅公钥登录，禁止 root，仅允许 $DEPLOY_USER 与 $OPS_USER"

# --- sudo 权限 ----------------------------------------------------------
#
# 运维账户：正常 sudo。
# 部署账户：不给通用 sudo。部署脚本本身以 root 运行（要写 /opt 与 /var/lib），
# 它通过 authorized_keys 的 command= 触发，不需要 sudo 条目。
# 这里额外给几条只读/重启命令，方便人肉排障。

cat >/etc/sudoers.d/90-dec-cloud-ops <<EOF
$OPS_USER ALL=(ALL) NOPASSWD:ALL
EOF
chmod 440 /etc/sudoers.d/90-dec-cloud-ops
visudo -cf /etc/sudoers.d/90-dec-cloud-ops >/dev/null || die "sudoers 校验失败"

cat >/etc/sudoers.d/91-dec-cloud-deploy <<EOF
# 部署账户只能以 root 执行部署入口本身：入口要写 $APP_DIR 与 $DATA_DIR，
# 以普通用户跑会在「初始化」步骤就被拒（真实踩过）。
# 入口仍是唯一入口：authorized_keys 的 command= 锁死到它，agent 转发与 PTY 全关，
# 因此即便私钥泄漏，攻击面也只是「触发一次部署」，不是「拿到 root shell」。
$DEPLOY_USER ALL=(root) NOPASSWD: $ENTRY_PATH, \\
    /bin/systemctl restart $SERVICE_NAME, /bin/systemctl status $SERVICE_NAME, \\
    /bin/systemctl is-active $SERVICE_NAME
EOF
chmod 440 /etc/sudoers.d/91-dec-cloud-deploy
visudo -cf /etc/sudoers.d/91-dec-cloud-deploy >/dev/null || die "sudoers 校验失败"

# --- 自动安全更新（需求 16.2）------------------------------------------

if command -v apt-get >/dev/null 2>&1; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq unattended-upgrades fail2ban curl ca-certificates python3
  cat >/etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
EOF
  systemctl enable --now unattended-upgrades >/dev/null 2>&1 || true
elif command -v dnf >/dev/null 2>&1; then
  # 区分必需与可选：curl/python3 是部署脚本的依赖，缺了必须失败；
  # fail2ban 与 dnf-automatic 在部分最小化镜像里没有对应源，
  # 一次装全部会让整条命令失败，进而让整个 provision 中断。
  dnf install -y -q curl ca-certificates python3 || die "缺少必需包 curl/ca-certificates/python3"
  dnf install -y -q dnf-automatic >/dev/null 2>&1 || log "dnf-automatic 不可用，跳过自动更新配置"
  dnf install -y -q fail2ban >/dev/null 2>&1 || log "fail2ban 不可用，跳过"
  if [ -f /etc/dnf/automatic.conf ]; then
    sed -i 's/^apply_updates.*/apply_updates = yes/' /etc/dnf/automatic.conf
    systemctl enable --now dnf-automatic.timer >/dev/null 2>&1 || true
  fi
fi
log "已开启自动安全更新"

# --- fail2ban（需求 16.3a）---------------------------------------------

if [ -d /etc/fail2ban ]; then
  cat >/etc/fail2ban/jail.d/dec-cloud-sshd.local <<'EOF'
[sshd]
enabled = true
maxretry = 3
findtime = 10m
bantime = 1h
EOF
  systemctl enable --now fail2ban >/dev/null 2>&1 || systemctl restart fail2ban || true
  log "fail2ban 已启用（sshd，10 分钟内 3 次失败封 1 小时）"
fi

# --- systemd 单元 -------------------------------------------------------

cat >/etc/systemd/system/$SERVICE_NAME.service <<EOF
[Unit]
Description=Dec Cloud
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_USER
# 服务端要持有 443，因此需要 CAP_NET_BIND_SERVICE。
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=yes
ExecStart=$APP_DIR/dec-cloud -data-dir $DATA_DIR -listen :443 -domain $DOMAIN -root-key $ROOT_KEY_DIR/root.key
Restart=on-failure
RestartSec=5s
# 部署脚本会 stop/start，给数据迁移留出时间。
TimeoutStopSec=30s

# 加固：只允许写数据目录与根密钥目录。
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=$DATA_DIR $ROOT_KEY_DIR

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable $SERVICE_NAME >/dev/null 2>&1 || true
log "systemd 单元已写入并启用（二进制未就位前不会启动）"

# --- 收尾 ---------------------------------------------------------------

chown -R "$SERVICE_USER:$SERVICE_USER" "$DATA_DIR"
chmod 750 "$DATA_DIR"

cat <<EOF

[provision] 完成。下一步：
  1. 安装部署入口（必须 root 所有、755，且不能有 group/other 写权限——它由 sudo 以 root 执行）：
     install -m 755 -o root -g root dec-cloud-deploy.sh $ENTRY_PATH
  2. 填 GitHub 只读 token：echo '<token>' > $ROOT_KEY_DIR/github-token && chmod 600 $ROOT_KEY_DIR/github-token
     （fine-grained token，只给 shichao402/Dec 的 Contents: Read 权限）
  3. 首次启动：systemctl start $SERVICE_NAME
  4. 初始化需要 Bitwarden 主密码与 2FA，必须由本人操作（需求 10.1）
  5. 根密钥位置：$ROOT_KEY_DIR/root.key（不在 $DATA_DIR 里，因此不进快照）
EOF
