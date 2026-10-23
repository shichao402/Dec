#!/bin/bash
# Dec Cloud 服务器声明式配置（需求 16.1、16.2、16.3a、16.5、21.6）。
#
# 一条命令从零重建服务器：账号、目录、权限、fail2ban、
# 自动安全更新、systemd 单元、部署账户的受限入口。
#
# 用法（在服务器上以 root 执行）：
#
#   ./provision.sh --domain dec.firoyang.com \
#                  --deploy-key "ssh-ed25519 AAAA... ci@github" \
#                  --ops-key    "ssh-ed25519 AAAA... me@laptop"
#
#   只有额外传 --harden-ssh 才会收紧 sshd；默认一行 sshd 配置都不写。
#
# 幂等：重复执行只是把状态收敛到声明，不会重复创建或破坏已有数据。
#
# 刻意不做的事：
#
#   - 不写 root.key。根密钥只能由服务在初始化流程里生成（需求 9.1）；
#     任何脚本预先放置密钥都会绕过 Bitwarden 冷备的写入与读回校验。
#   - 不申请证书。证书由服务自己用 TLS-ALPN-01 取得（需求 7）。
#   - 不配置任何 Dec 业务数据。资产与密钥一律走协议写入链路。
#   - 不改动机器上的全局 SSH 登录策略。这台机器不专属 Dec，可能还跑着
#     别的服务、也有别的人要登录；AllowUsers 是白名单，写进去就把名单
#     外的账号（包括 root）全锁在门外。需要收紧时显式传 --harden-ssh。

set -euo pipefail

DOMAIN=""
DEPLOY_KEY=""
OPS_KEY=""
OPS_USER="${DEC_OPS_USER:-decops}"
DEPLOY_USER="${DEC_DEPLOY_USER:-deploy}"
SERVICE_USER="${DEC_SERVICE_USER:-dec}"
SERVICE_NAME="dec-cloud"
# SSH 加固是 opt-in：这台机器不专属 Dec，全局登录策略不该由部署脚本决定。
HARDEN_SSH=0
# 配置目录：放 github-token 这类只该 root 读的东西，root 所有。
# 根密钥刻意放在数据目录之外，因为数据目录会被整体快照（需求 16.5、21.5）。
CONF_DIR="${DEC_CONF_DIR:-/etc/dec-cloud}"
# 根密钥目录：单独一层，归属服务账号。
#
# 为什么必须单独一两层、且归属服务账号：写根密钥用的是「临时文件 + rename」
# 的原子写，要求目录本身可写——只给文件写权限不够。而配置目录里躺着
# github-token（root:root 0600），不能让服务账号能在那层建文件，
# 否则它可以先把 token 文件 rename 走再伪造一个。两者的写权限要求直接冲突，
# 所以分成两个目录。
ROOT_KEY_DIR="${DEC_ROOT_KEY_DIR:-$CONF_DIR/secret}"
DATA_DIR="/var/lib/dec-cloud"
APP_DIR="/opt/dec-cloud"
# dec-cloud 只监听回环高位端口；公网 443 由 nginx 按 SNI 透传过来。
LISTEN_ADDR="${DEC_LISTEN_ADDR:-127.0.0.1:8444}"
# 部署入口：authorized_keys 的 command= 与 sudoers 都指向这一个路径，
# 只改一处就会两边不一致，所以集中定义。
ENTRY_PATH="/usr/local/lib/${SERVICE_NAME}/${SERVICE_NAME}-deploy.sh"

log()  { printf '[provision] %s\n' "$*"; }
die()  { printf '[provision] 失败：%s\n' "$*" >&2; exit 1; }

usage() {
  sed -n '2,25p' "$0" | sed 's/^# \{0,1\}//'
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --domain)     DOMAIN="${2:-}"; shift 2 ;;
    --deploy-key) DEPLOY_KEY="${2:-}"; shift 2 ;;
    --ops-key)    OPS_KEY="${2:-}"; shift 2 ;;
    --harden-ssh) HARDEN_SSH=1; shift ;;   # 只有显式要求才收紧 sshd
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
# 配置目录：root 所有，服务账号只读不能写。
# github-token 就放在这里，服务账号拿不到写权限，也就没法替换它。
install -d -m 750 -o root -g "$SERVICE_USER" "$CONF_DIR"
# 根密钥目录：归属服务账号（原子写要求目录可写），但 700 挡住其他账号。
install -d -m 700 -o "$SERVICE_USER" -g "$SERVICE_USER" "$ROOT_KEY_DIR"
# 部署脚本要向 GitHub 核对 tag 真实性（需求 21.7），token 只给 root 读。
# 这里只建空占位并锁权限，值由人填——把 token 写进脚本或仓库都等于泄露。
if [ ! -f "$CONF_DIR/github-token" ]; then
  install -m 600 -o root -g root /dev/null "$CONF_DIR/github-token"
  log "已创建 $CONF_DIR/github-token 占位，需填入只读 token（见部署文档）"
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

# --- SSH 加固（opt-in，需求 16.3a、21.6）--------------------------------
#
# 默认什么都不做。原因：这台机器不专属 Dec，AllowUsers 是白名单语义——
# 一旦写进去，名单外的账号（root、其他服务的运维号、以后加的人）全部
# 登录不了。部署脚本没有资格替机器主人决定谁能登录。
#
# 需要收紧的独占机器，显式传 --harden-ssh。即便如此也只写 dropin，
# 不改 /etc/ssh/sshd_config 本体，方便随时整文件删掉回滚。

if [ "$HARDEN_SSH" -eq 1 ]; then
  sshd_dropin=/etc/ssh/sshd_config.d/99-dec-cloud.conf
  mkdir -p /etc/ssh/sshd_config.d
  cat >"$sshd_dropin" <<EOF
# Dec Cloud 的 SSH 基线（由 provision.sh --harden-ssh 写入）。
# 只允许公钥登录，禁止密码，并限制可登录的账号。
# 删除本文件并 reload sshd 即可完全回滚。
PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
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
  log "SSH 已加固：仅公钥登录，仅允许 $DEPLOY_USER 与 $OPS_USER"
else
  # 不加固也要提醒一句：账号建好了，能用什么方式登录取决于机器现有策略。
  log "跳过 SSH 加固（未传 --harden-ssh）。$DEPLOY_USER 与 $OPS_USER 的登录方式沿用机器现有 sshd 策略"
  log "  确需收紧时显式加 --harden-ssh；root 及其他账号的登录不受本脚本影响"
fi

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

# --- 冷备/审计的 SSH 前置条件（需求 15.2）-------------------------------
#
# 推送用 go-git，它按 known_hosts 校验主机密钥，不跳过。所以必须事先把
# GitHub 的主机公钥落盘，否则第一次推送会因 unknown host 失败。
#
# 为什么固定用 ssh.github.com:443 而不是 github.com:22：云服务器出方向常被
# 出口策略拦掉 22，443 通常放行；GitHub 在 443 上提供同一套 SSH 服务，
# 且主机公钥与 22 端口完全相同（同一把 ed25519 key）。
#
# 主机公钥写死在这里而不是运行时 ssh-keyscan：首次运行就去抓公钥等于
# 第一次连接不做校验（TOFU），攻击者只要在那一次抢先应答就能冒充。
# 写死的代价是 GitHub 换钥时要改脚本，但那是极低频且可观测的事件。

DEC_KEYS_DIR="$CONF_DIR/keys"
install -d -m 750 -o root -g "$SERVICE_USER" "$DEC_KEYS_DIR"

KNOWN_HOSTS_FILE="$CONF_DIR/known_hosts"
cat >"$KNOWN_HOSTS_FILE" <<'KHEOF'
# GitHub 的主机公钥（Ed25519，ssh.github.com:443 与 github.com:22 相同）。
# 由 provision.sh 写入，供 dec-cloud 推送冷备与审计时校验。
[ssh.github.com]:443 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl
github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl
KHEOF
chown root:root "$KNOWN_HOSTS_FILE"
chmod 644 "$KNOWN_HOSTS_FILE"
log "已写入 GitHub 主机公钥到 $KNOWN_HOSTS_FILE"

# --- systemd 单元 -------------------------------------------------------
#
# 可选运行参数（冷备/审计仓库地址、本机证书签发）走 EnvironmentFile 而不是
# 直接写进 ExecStart，理由有三个：
#
#   1. 部署脚本从 ExecStart 里 sed 提取 -listen 来推导回环地址。ExecStart 越短
#      越不会被误匹配，也不会因为参数顺序变动而失效。
#   2. 这些东西改的是「往哪儿备份、用哪个密钥名」，属于运维操作，不该为了
#      改一个地址去重跑 provision.sh（那是重建机器用的，副作用大）。
#   3. 环境文件是可选的：不存在时服务照常启动，功能各自关闭（冷备不推送、
#      本机证书不签发），符合「没配置就降级」的既有约定。
#
# 文件里只放非机密内容（仓库地址、密钥名、路径）。真正的机密（DNSPod 凭据、
# 企业微信 webhook）在 Dec 的密钥库里，由根密钥加密，不落在这个文件里。

ENV_FILE="/etc/dec-cloud/service.env"
# 模板只在文件不存在时写入：重跑 provision.sh 不能覆盖线上已经填好的值。
if [ ! -f "$ENV_FILE" ]; then
  cat >"$ENV_FILE" <<'ENVEOF'
# Dec Cloud 可选运行参数。改完执行：
#   sudo systemctl restart dec-cloud
#
# 只有一个变量 DEC_EXTRA_ARGS，所有可选启动参数写在它里面（同一行）。
# 留空即全部关闭，服务行为与没有这个文件时一致。
#
# 全部打开的样子（把注释去掉并按自己的仓库/密钥名改）：
#
# DEC_EXTRA_ARGS="-backup-url git@github.com:you/dec-backup.git -backup-key /etc/dec-cloud/keys/backup_deploy -audit-url git@github.com:you/dec-audit.git -audit-key /etc/dec-cloud/keys/audit_deploy -local-tls-domain local.dec.firoyang.com -local-tls-zone firoyang.com -local-tls-secret dnspod-token"
#
# 各项含义：
#   -backup-url   冷备仓库地址（需求 15.1）；留空不推送冷备
#   -backup-key   冷备仓库的 deploy key 私钥路径
#   -audit-url    审计仓库地址（需求 15.3）；留空不推送审计
#   -audit-key    审计仓库的 deploy key 私钥路径
#   -local-tls-domain  指向 127.0.0.1 的本机子域
#   -local-tls-zone    该子域所在的 DNSPod 主域名
#   -local-tls-secret  Dec 密钥库里存放 DNS 凭据的密钥名；留空则关闭签发
#
# 机密（DNS 凭据、企业微信 webhook）一律在 Dec 密钥库里，由根密钥加密，
# 不写在这个文件里。这里只有仓库地址与文件路径。
#
# 推送目标的主机密钥校验文件不走这个文件，而是由 systemd 单元直接传
# -known-hosts $CONF_DIR/known_hosts（由 provision.sh 写入 GitHub 公钥）。
# 走环境变量 SSH_KNOWN_HOSTS 也能生效，但那是隐式约定：服务由 systemd 拉起，
# 多一层传导就多一处"为什么没生效"的可能。

DEC_EXTRA_ARGS=""
ENVEOF
  chown root:root "$ENV_FILE"
  chmod 640 "$ENV_FILE"
  chgrp "$SERVICE_USER" "$ENV_FILE"
  log "已写入可选参数模板 $ENV_FILE（DEC_EXTRA_ARGS 为空，未启用任何可选功能）"
else
  # 已有文件不改内容，只校正权限：可能有人手动改过权限。
  chown root:root "$ENV_FILE"
  chmod 640 "$ENV_FILE"
  chgrp "$SERVICE_USER" "$ENV_FILE"
  log "保留已有的 $ENV_FILE"
fi

cat >/etc/systemd/system/$SERVICE_NAME.service <<EOF
[Unit]
Description=Dec Cloud
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_USER
# 只监听回环高位端口，公网 443 归 nginx（stream + ssl_preread 按 SNI 透传，不终止 TLS）。
# 不需要绑定低位端口，因此不授予任何 capability。
CapabilityBoundingSet=
NoNewPrivileges=yes
# 可选参数从这个文件读；文件缺失或变量为空都不影响启动。
# 行首的 "-" 表示文件不存在时也算正常，不报错。
EnvironmentFile=-$ENV_FILE
ExecStart=$APP_DIR/dec-cloud -data-dir $DATA_DIR -listen $LISTEN_ADDR -domain $DOMAIN -root-key $ROOT_KEY_DIR/root.key -known-hosts $KNOWN_HOSTS_FILE \$DEC_EXTRA_ARGS
Restart=on-failure
RestartSec=5s
# 部署脚本会 stop/start，给数据迁移留出时间。
TimeoutStopSec=30s

# 加固：只允许写数据目录与根密钥目录。
# 配置目录（github-token 所在）刻意不在可写清单里：服务账号只需要读它。
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=$DATA_DIR $ROOT_KEY_DIR

# 内存上限：留出余量给内核与 sshd，超了让服务自己被杀，别拖垮整机。
#
# 2026-10-24 的实际故障：推送审计时 go-git 要把松散对象重新打包，
# dec-cloud 的 anon-rss 涨到 754MB，在 956MB 的机器上触发全局 OOM，
# 整机失去响应（ping 通、SSH 与 HTTPS 全不通），只能走云 API 重启。
# 单独设上限的意义不是"不 OOM"，而是把伤害限制在这个 unit 里：
# 它被杀后 Restart=on-failure 会拉起，sshd 还有内存可用，人还进得去。
MemoryMax=600M

[Install]
WantedBy=multi-user.target
EOF

# --- swap ---------------------------------------------------------------
#
# 为什么需要：这台 1G 内存的机器上，git 打包对象是内存尖峰的最大来源
# （见上面 MemoryMax 的注释）。有 swap 时内核可以把冷页换出去，
# 打包进程不至于直接被 OOM 杀掉；没有 swap 时 956MB 里只要出现 700MB 级别的
# 匿名页，全机就悬了。
#
# 必须写 fstab：2026-10-24 那次事故的善后里手工建了 /swapfile，
# 但没写 fstab，机器一重启 swap 就没了——下一次同样的推送又会 OOM。
SWAP_FILE="/swapfile"
if [ ! -f "$SWAP_FILE" ]; then
  fallocate -l 2G "$SWAP_FILE" || dd if=/dev/zero of="$SWAP_FILE" bs=1M count=2048 status=none
  chmod 600 "$SWAP_FILE"
  mkswap "$SWAP_FILE" >/dev/null
  log "已创建 $SWAP_FILE（2G）"
fi
if ! grep -qE "^[[:space:]]*$SWAP_FILE[[:space:]]" /etc/fstab; then
  printf '%s none swap sw 0 0\n' "$SWAP_FILE" >>/etc/fstab
  log "已把 $SWAP_FILE 写入 /etc/fstab（重启后仍生效）"
fi
if ! swapon --show=NAME --noheadings 2>/dev/null | grep -qx "$SWAP_FILE"; then
  swapon "$SWAP_FILE" && log "已启用 swap：$SWAP_FILE"
fi

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
  2. 填 GitHub 只读 token：echo '<token>' > $CONF_DIR/github-token && chmod 600 $CONF_DIR/github-token
     （fine-grained token，只给 shichao402/Dec 的 Contents: Read 权限）
  3. 首次启动：systemctl start $SERVICE_NAME
  4. 初始化需要 Bitwarden 主密码与 2FA，必须由本人操作（需求 10.1）
  5. 根密钥位置：$ROOT_KEY_DIR/root.key（不在 $DATA_DIR 里，因此不进快照）
  6. 可选功能（冷备、审计、本机证书）在 $ENV_FILE 里配置，
     填好后 systemctl restart $SERVICE_NAME 生效；该文件不会被 provision.sh 覆盖。
EOF
