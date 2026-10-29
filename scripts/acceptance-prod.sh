#!/usr/bin/env bash
# acceptance-prod.sh 在生产服务器上检查线上真实状态。
#
# 为什么必须跑在服务器上：这份检查的每一条都是「此刻那台机器的事实」——
# 服务在不在、监听哪些端口、证书什么时候到期、资产库里有没有残留。
# 在本机跑只能看到缓存与副本，那正是上一轮验收的毛病。
#
# 由 scripts/acceptance.sh --remote 通过 SSH 送上去执行（bash -s 从 stdin 读），
# 不在服务器上留任何文件。全部检查只读：唯一的写操作是一句 hello，
# 它只更新「这台机器最后一次在线时间」。
set -uo pipefail

# 这份检查断言的是「生产服务器上的事实」：进程在不在、证书什么时候到期、
# 资产目录里有没有残留。这些只有跑在服务器上才成立。
#
# 在本机直接跑不会连不上，而是把宿主的 OpenSSL 版本、BSD date、
# 缺失的免密 sudo 逐条算成 FAIL——看起来像线上出了故障，实际只是跑错了
# 地方。上一轮就因此得出过「生产有 6 项异常」的错误结论。
#
# 所以先确认脚下这台机器确实是服务器，不是就停下来。
if [ "$(uname -s)" != "Linux" ]; then
	echo "这份检查只能跑在生产服务器上（当前是 $(uname -s)，像是你的本机）。" >&2
	echo >&2
	echo "对生产做线上验收请用：" >&2
	echo "  scripts/acceptance.sh --remote" >&2
	echo >&2
	echo "只想在本机代理上验 MCP 链路与落盘，用：scripts/acceptance.sh" >&2
	exit 2
fi

# 远端必须是 GNU date：证书过期那条断言要用 `date -d` 解析 OpenSSL 的
# notAfter，BSD date 没有 -d，会把「解析不了」静默算成 0，进而报成已过期。
if ! date -d "2027-01-01" +%s >/dev/null 2>&1; then
	echo "需要 GNU date（当前 date 不支持 -d），证书到期那条断言会误判。" >&2
	exit 2
fi

DOMAIN="${1:-dec.firoyang.com}"
BASE="https://${DOMAIN}"

pass=0
fail=0
skipped=0
failed_names=()
skipped_names=()

ok() { printf '  [PASS] %s — %s\n' "$1" "$2"; pass=$((pass + 1)); }
bad() { printf '  [FAIL] %s — %s\n' "$1" "$2"; fail=$((fail + 1)); failed_names+=("$1"); }
# info 是纯说明：这一项本就不需要结论（例如"当前 bundle 列表"），
# 单独列出来是为了让人核对，不是没验到。
info() { printf '  [····] %s — %s\n' "$1" "$2"; }
# skip 是「本该有结论但没拿到」：权限、工具缺失之类。
# 它必须单独计数——既不能算通过（那样验收会显得比实际覆盖得更全），
# 也不能和 info 混在一起（那样真正的覆盖缺口会被一堆说明条目淹掉）。
skip() {
	printf '  [SKIP] %s — %s\n' "$1" "$2"
	skipped=$((skipped + 1))
	skipped_names+=("$1")
}

# 断言「期望值等于实际值」。空实际值单独提示，避免把「没取到」说成「不相等」。
need() {
	local name="$1" want="$2" got="$3" hint="${4:-}"
	if [ "$want" = "$got" ]; then
		ok "$name" "$got"
	elif [ -z "$got" ]; then
		bad "$name" "没有取到值${hint:+（${hint}）}"
	else
		bad "$name" "期望 ${want}，实际 ${got}"
	fi
}

# call <method> [payload-json] [requested_version]
# 协议信封的 payload 是 base64 后的原始字节，不是 JSON 对象。
call() {
	local m="$1" p="${2:-{\}}" v="${3:-}" b body
	b=$(printf '%s' "$p" | base64 -w0 2>/dev/null || printf '%s' "$p" | base64 | tr -d '\n')
	body="{\"id\":\"acc\",\"protocol\":1,\"method\":\"${m}\",\"payload\":\"${b}\""
	[ -n "$v" ] && body="${body},\"requested_version\":\"${v}\""
	body="${body}}"
	curl -sS --max-time 20 -X POST "${BASE}/api" -H 'content-type: application/json' -d "$body" 2>/dev/null
}

# jget <json> <key>：取顶层字段，缺失时输出空串。
jget() { printf '%s' "$1" | python3 -c 'import sys,json
try:
    print(json.load(sys.stdin).get(sys.argv[1], ""))
except Exception:
    print("")' "$2" 2>/dev/null; }

# decode_payload <response>：把响应的 payload 从 base64 解回 JSON 文本。
decode_payload() {
	printf '%s' "$1" | python3 -c 'import sys,json,base64
try:
    d = json.load(sys.stdin)
    p = d.get("payload") or ""
    print(base64.b64decode(p).decode() if p else "{}")
except Exception:
    print("{}")' 2>/dev/null
}

echo "生产侧验收：${DOMAIN}"
echo "主机：$(hostname)  时间：$(date -u '+%Y-%m-%dT%H:%M:%SZ')"

# ---------------------------------------------------------------- 一、服务
echo
echo "一、服务"
# systemctl 读不到时记跳过而不是失败：与这台机器的权限配置有关，
# 与 Dec 服务是否健康无关。此前读不到会被算成 FAIL，看起来像服务挂了。
svc_active=$(systemctl is-active dec-cloud 2>/dev/null || true)
if [ -n "$svc_active" ]; then
	need "dec-cloud 正在运行" "active" "$svc_active"
else
	skip "dec-cloud 正在运行" "跳过（读不到 systemctl 状态）"
fi
svc_enabled=$(systemctl is-enabled dec-cloud 2>/dev/null || true)
if [ -n "$svc_enabled" ]; then
	need "dec-cloud 已设开机自启" "enabled" "$svc_enabled"
else
	skip "dec-cloud 已设开机自启" "跳过（读不到 systemctl 状态）"
fi

# 对外暴露面：只允许 22、80、443 三个公网监听。
#
# 80 是放行而非遗漏：这台是公共机器，同机还有 relkit 等站点，80 由 nginx
# 统一接管（HTTP 跳 HTTPS、certbot HTTP-01 续期），后面不直通任何服务。
# Dec 自身不依赖 80。
ports=$(sudo -n ss -lnt 2>/dev/null | awk 'NR > 1 && $4 !~ /^127\./ && $4 !~ /^\[::1\]/ {print $4}' |
	sed 's/.*://' | sort -un | tr '\n' ' ' | sed 's/ *$//')
if [ -z "$ports" ]; then
	skip "公网监听端口" "跳过（读不到 socket 表，需要免密 sudo）"
else
	need "对外只开 22、80（公共机器，nginx 接管）与 443" "22 80 443" "$ports"
fi

# ---------------------------------------------------------------- 二、协议
echo
echo "二、协议与版本"
health=$(curl -sS --max-time 15 "${BASE}/healthz" 2>/dev/null)
need "healthz 报告正常" "True" "$(jget "$health" ok)"
need "healthz 协议版本" "1" "$(jget "$health" protocol)"
need "healthz 未处于维护" "False" "$(jget "$health" maintenance)"
srv_version=$(jget "$health" server_version)
required=$(jget "$health" required_client)
# required 的语义是「最低可用版本」，不是「必须等于服务端版本」：只改服务端的
# 发布会让 CI 沿用上一个 required（需求 19.2），两者本来就不该相等。这里验的是
# required 不高于服务端版本——反过来说，required 更高就等于把所有代理都挡在门外。
if [ -z "$required" ] || [ -z "$srv_version" ]; then
	bad "要求版本不高于服务端版本" "没有取到值（required=${required:-空} server=${srv_version:-空}）"
elif [ "$(printf '%s\n%s\n' "$required" "$srv_version" | sort -V | head -n1)" = "$required" ]; then
	ok "要求版本不高于服务端版本" "required=${required} server=${srv_version}"
else
	bad "要求版本不高于服务端版本" "required=${required} 高于 server=${srv_version}，会让所有代理被判不对齐"
fi
info "服务端版本" "${srv_version:-未取到}"

# hello 是唯一允许无证书调用的方法：它是「未登记的新机器也能问出要求版本」的入口。
hello=$(call hello '{"agent_version":"'"${required}"'"}')
need "hello 免证书可调用" "True" "$(jget "$hello" ok)"
hello_body=$(decode_payload "$hello")
need "hello 报告本机已对齐" "True" "$(jget "$hello_body" aligned)"
need "hello 返回的服务端版本" "$srv_version" "$(jget "$hello_body" server_version)"

# 版本不对齐必须给出可定位的原因，而不是空结果。
mismatch=$(call list_assets '{}' 'v0.0.1')
need "旧版本被拦下" "version_mismatch" "$(jget "$mismatch" code)"
need "旧版本错误里带上要求版本" "$required" "$(jget "$mismatch" required_version)"

# ---------------------------------------------------------------- 三、鉴权
echo
echo "三、鉴权边界（无设备证书）"
for m in status sync list_assets read_asset write_asset delete_bundle \
	list_secrets read_secret put_secret history devices revoke_device \
	set_device_kind rollback rotate_root_key; do
	need "${m} 被拒绝" "unauthorized" "$(jget "$(call "$m" '{}')" code)"
done

# MCP 与代理管理面只存在于代理的本机回环上，公网不该出现（否则等于把入口开给了全网）。
for p in /mcp /admin; do
	code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 15 "${BASE}${p}" 2>/dev/null)
	need "${p} 未暴露在公网" "404" "$code"
done

# ---------------------------------------------------------------- 四、TLS
echo
echo "四、TLS"
cert=$(echo | openssl s_client -connect "${DOMAIN}:443" -servername "${DOMAIN}" 2>/dev/null |
	openssl x509 -noout -subject -issuer -enddate 2>/dev/null)
# subject 只核对域名本身，不比对整行：
# OpenSSL 1.x/3.x 输出 `subject=CN = dec.firoyang.com`，
# LibreSSL 输出 `subject= /CN=dec.firoyang.com`。
# 写死前者的整行会让同一份脚本在另一类机器上必然误判为「证书主体不对」，
# 而它真正要守的只是「这张证书属于本站」。
subject=$(printf '%s' "$cert" | sed -n 's/^subject=//p')
if printf '%s' "$subject" | grep -q "${DOMAIN}"; then
	ok "证书主体是本站域名" "${subject}"
else
	bad "证书主体是本站域名" "${subject:-未取到 subject}"
fi
issuer=$(printf '%s' "$cert" | grep '^issuer=')
if printf '%s' "$issuer" | grep -q "Let's Encrypt"; then
	ok "证书由 Let's Encrypt 签发" "issuer 匹配"
else
	bad "证书由 Let's Encrypt 签发" "实际 ${issuer:-未取到}"
fi
not_after=$(printf '%s' "$cert" | sed -n 's/^notAfter=//p')
if [ -n "$not_after" ] && [ "$(date -d "$not_after" +%s 2>/dev/null || echo 0)" -gt "$(date +%s)" ]; then
	ok "证书尚未过期" "$not_after"
else
	bad "证书尚未过期" "${not_after:-未取到到期时间}"
fi

# ---------------------------------------------------------------- 五、加固
echo
echo "五、SSH 加固"
sshd=$(sudo -n sshd -T 2>/dev/null)
if [ -z "$sshd" ]; then
	skip "sshd 有效配置" "跳过（读不到，需要免密 sudo）"
else
	# 这台机器不专属 Dec，provision 默认不写 sshd 配置，也不该由部署脚本
	# 决定谁能登录。所以这里不断言 root 与 AllowUsers，只确认两件与 Dec
	# 自身安全相关的事：公钥可用、密码不可用。
	need "公钥登录已开启" "yes" "$(printf '%s' "$sshd" | awk '/^pubkeyauthentication /{print $2}')"
	need "禁止密码登录" "no" "$(printf '%s' "$sshd" | awk '/^passwordauthentication /{print $2}')"
	info "root 登录策略" "$(printf '%s' "$sshd" | awk '/^permitrootlogin /{print $2}' | sed 's/^$/未设置（默认 prohibit-password）/')"
	allow=$(printf '%s' "$sshd" | awk '/^allowusers /{print $2}' | sort -u | tr '\n' ' ' | sed 's/ *$//')
	info "AllowUsers" "${allow:-未设置（不限制账号）}"
fi

# ---------------------------------------------------------------- 六、数据
echo
echo "六、资产与审计"
if sudo -n test -d /var/lib/dec-cloud/ssot/assets 2>/dev/null; then
	bundles=$(sudo -n ls /var/lib/dec-cloud/ssot/assets 2>/dev/null | grep -v '^\.' | sort | tr '\n' ' ' | sed 's/ *$//')
	info "当前 bundle" "${bundles:-空}"
	# 验收用的临时 bundle 一律以 dec-acceptance- 开头，跑完必须一个不剩。
	residue=$(printf '%s\n' "$bundles" | tr ' ' '\n' | grep '^dec-acceptance' | tr '\n' ' ' | sed 's/ *$//')
	if [ -z "$residue" ]; then
		ok "没有验收残留 bundle" "—"
	else
		bad "没有验收残留 bundle" "残留：$residue"
	fi
	# 不盯具体 bundle 名：资产由用户自己决定，验收只保证目录可读、
	# 且没有本次跑出来的残留。生产上有没有内容不是这项检查的职责。
	if [ -n "$bundles" ]; then
		ok "资产目录可读且有内容" "—"
	else
		info "资产目录可读但为空" "空仓库不是缺陷"
	fi
else
	# 读不到是权限问题，不是残留问题：此前会算成 FAIL，
	# 把「没权限看」说成「线上有残留」。
	skip "资产目录可读" "跳过（读不到，需要免密 sudo）"
fi

audit_head=$(sudo -n git -c safe.directory=/var/lib/dec-cloud/audit \
	-C /var/lib/dec-cloud/audit log -1 --format=%h 2>/dev/null)
if [ -n "$audit_head" ]; then
	ok "审计仓库有提交" "$audit_head"
else
	# 同理：读不到提交历史 ≠ 仓库没提交，可能只是没权限。
	skip "审计仓库有提交" "跳过（读不到，需要免密 sudo）"
fi

# 凭据明文（credential-landing 需求 8.4）：私钥与项目 secret 只能以密文进 SSOT。
# 本机验收（acceptance.sh）会写一对临时私钥和一个以固定前缀开头的 secret，
# 前缀与 internal/acceptance/credential.go 的 PlaintextMarker 必须一致。
# 只搜文本：certs/ 下的 ca.key 是合法私钥，不在扫描范围内；.git 对象是压缩的，交给 git log -G。
plain_re='BEGIN [A-Z ]*PRIVATE KEY|DEC-ACCEPTANCE-PLAINTEXT-'
data=/var/lib/dec-cloud
plain_files=$(for d in ssot state audit; do
	sudo -n test -d "$data/$d" 2>/dev/null || continue
	sudo -n grep -rIlE --exclude-dir=.git "$plain_re" "$data/$d" 2>/dev/null
done | sed "s#^$data/##" | tr '\n' ' ' | sed 's/ *$//')
# 扫描有可能一次都没跑起来（免密 sudo 缺失会把三个目录全部 continue 掉），
# 此时结果空字符串并不代表"干净"，而是"什么都没查"。
# 这条断言的结论是安全结论，宁可明确说没查到，也不能报成通过。
scanned=$(for d in ssot state audit; do
	sudo -n test -d "$data/$d" 2>/dev/null && echo "$d"
done | tr '\n' ' ' | sed 's/ *$//')
if [ -z "$scanned" ]; then
	skip "数据目录无明文凭据" "跳过（ssot、state、audit 都读不到，扫描未执行）"
elif [ -z "$plain_files" ]; then
	ok "数据目录无明文凭据" "$scanned"
else
	bad "数据目录无明文凭据" "命中：$plain_files"
fi
plain_commits=""
scanned_repos=""
for d in ssot audit; do
	# 目录都进不去时该仓库就没被扫过，不能算进"无命中"。
	sudo -n test -d "$data/$d" 2>/dev/null || continue
	scanned_repos="$scanned_repos $d"
	hits=$(sudo -n git -c safe.directory="$data/$d" -C "$data/$d" log --all -E -G"$plain_re" --format=%h 2>/dev/null | tr '\n' ' ' | sed 's/ *$//')
	[ -n "$hits" ] && plain_commits="$plain_commits $d:$hits"
done
if [ -z "$scanned_repos" ]; then
	skip "git 历史无明文凭据" "跳过（ssot、audit 都读不到，扫描未执行）"
elif [ -z "$plain_commits" ]; then
	ok "git 历史无明文凭据" "${scanned_repos# } 全部提交"
else
	bad "git 历史无明文凭据" "命中提交：${plain_commits# }"
fi
secret_residue=$(sudo -n ls "$data/ssot/secrets" 2>/dev/null | grep '^dec-acceptance' | tr '\n' ' ' | sed 's/ *$//')
if [ -z "$secret_residue" ]; then
	ok "没有验收残留密钥" "—"
else
	bad "没有验收残留密钥" "残留：$secret_residue"
fi

# ---------------------------------------------------------------- 七、分发
echo
echo "七、分发完整性"
if sudo -n test -f /var/lib/dec-cloud/release.json 2>/dev/null; then
	# 发布清单是唯一权威：从它取期望的 sha256，与真实下载内容比对。
	curl -sS --max-time 60 -o /tmp/.dec-acc-agent "${BASE}/download/dec-agent-linux-amd64" 2>/dev/null
	actual=$(sha256sum /tmp/.dec-acc-agent 2>/dev/null | cut -d' ' -f1)
	want=$(sudo -n cat /var/lib/dec-cloud/release.json 2>/dev/null | python3 -c 'import sys,json
try:
    d = json.load(sys.stdin)
    for a in d.get("files", []):
        if a.get("name") == "dec-agent-linux-amd64":
            print(a.get("sha256", ""))
except Exception:
    print("")' 2>/dev/null)
	rm -f /tmp/.dec-acc-agent
	if [ -n "$actual" ] && [ "$actual" = "$want" ]; then
		ok "下载的代理与发布清单一致" "${actual:0:16}…"
	else
		bad "下载的代理与发布清单一致" "清单 ${want:-未取到}，实际 ${actual:-未取到}"
	fi
else
	skip "发布清单" "跳过（读不到 release.json）"
fi

# ---------------------------------------------------------------- 汇总
echo
# 跳过项必须显式报出来：此前它们不出现在「通过 N 项，失败 0 项」里，
# 于是「有几项因为权限没验到」和「全部验过且没问题」看起来一模一样。
echo "结果：通过 ${pass} 项，失败 ${fail} 项，跳过 ${skipped} 项"
if [ "$skipped" -gt 0 ]; then
	printf '未覆盖：%s\n' "${skipped_names[*]}"
	echo
	echo "跳过的项没有给出结论，不算通过。需要它们时补上免密 sudo 再跑一次。"
fi
if [ "$fail" -gt 0 ]; then
	printf '未通过：%s\n' "${failed_names[*]}"
	exit 1
fi
exit 0
