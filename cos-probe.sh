#!/usr/bin/env bash
# COS 出口链路探测：在目标服务器上运行，判断该机出口 IP 能否正常访问腾讯云广州 COS
#
# 用法：bash cos-probe.sh
# 只依赖 curl，无需密钥。预签名 URL 内置，24 小时有效（至 2026-10-24 03:06 UTC）。
set -uo pipefail

BUCKET="cosprobe1792724801-1251882798"
REGION="ap-guangzhou"

OBJ_URL="https://${BUCKET}.cos.${REGION}.myqcloud.com/probe/hello.txt?q-sign-algorithm=sha1&q-ak=AKIDWs2yiFYfcUBt20mMhpmaBZjwxMYVEk27&q-sign-time=1792724759;1792811219&q-key-time=1792724759;1792811219&q-header-list=&q-url-param-list=&q-signature=94d261d469c9c7f9bea6a39d9b2fb87cc20d6ceb"

LIST_URL="https://${BUCKET}.cos.${REGION}.myqcloud.com/?max-keys=10&prefix=probe%2F&q-sign-algorithm=sha1&q-ak=AKIDWs2yiFYfcUBt20mMhpmaBZjwxMYVEk27&q-sign-time=1792724759;1792811219&q-key-time=1792724759;1792811219&q-header-list=&q-url-param-list=max-keys;prefix&q-signature=b5788d9e1652e36bc6098afd8ae30f7ccda8b900"

SVC_URL="https://cos.${REGION}.myqcloud.com/?q-sign-algorithm=sha1&q-ak=AKIDWs2yiFYfcUBt20mMhpmaBZjwxMYVEk27&q-sign-time=1792724759;1792811219&q-key-time=1792724759;1792811219&q-header-list=&q-url-param-list=&q-signature=9e65074b66b4fcb68b59d9dfdc79f7c2d8c4d434"

hr() { printf '%s\n' "------------------------------------------------------------"; }

echo "COS 出口链路探测  $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
hr

echo "【0】本机出口公网 IP（COS 看到的就是它）"
for u in https://ifconfig.me/ip https://api.ipify.org https://ipinfo.io/ip; do
  ip=$(curl -s -m 8 "$u" 2>/dev/null | tr -d '[:space:]')
  if [ -n "$ip" ]; then echo "  出口 IP = $ip  (来源 $u)"; break; fi
done
hr

echo "【1】DNS 解析（看是否被劫持/就近调度）"
for h in "cos.${REGION}.myqcloud.com" "${BUCKET}.cos.${REGION}.myqcloud.com"; do
  printf '  %-45s -> ' "$h"
  getent hosts "$h" 2>/dev/null | awk '{print $1}' | paste -sd, - || echo "解析失败"
done
hr

echo "【2】TCP/TLS 连通性（4 个广州节点，看是否有节点不通）"
cosip=$(getent hosts "cos.${REGION}.myqcloud.com" 2>/dev/null | awk '{print $1}' | head -3)
for ip in $cosip; do
  printf '  %-16s ' "$ip"
  if curl -s -m 6 -o /dev/null --resolve "cos.${REGION}.myqcloud.com:443:${ip}" \
       "https://cos.${REGION}.myqcloud.com/" -w 'TLS_OK code=%{http_code} handshake=%{time_appconnect}s\n' 2>/dev/null; then :; else
    echo "连接失败/TLS 握手失败"
  fi
done
hr

echo "【3】签名对象 GET（关键：能读到内容 = 全链路正常）"
curl -sS -m 15 -w '\n  HTTP=%{http_code}  对端=%{remote_ip}  耗时=%{time_total}s\n' "$OBJ_URL" 2>&1 | sed 's/^/  /'
hr

echo "【4】签名桶 LIST"
curl -sS -m 15 -o /tmp/cos-list.xml -w '  HTTP=%{http_code}  对端=%{remote_ip}  耗时=%{time_total}s\n' "$LIST_URL" 2>&1
echo "  --- 返回体 ---"
sed 's/^/    /' /tmp/cos-list.xml 2>/dev/null | head -12
hr

echo "【5】签名 GetService（列账号全部桶，跨地域总览）"
curl -sS -m 15 -o /tmp/cos-svc.xml -w '  HTTP=%{http_code}  对端=%{remote_ip}  耗时=%{time_total}s\n' "$SVC_URL" 2>&1
echo "  --- 桶清单 ---"
grep -oE '<Name>[^<]*</Name>' /tmp/cos-svc.xml 2>/dev/null | sed 's/<[^>]*>//g' | sed 's/^/    /'
hr

echo "【6】匿名访问对照（应返回 403 AccessDenied，说明服务端正常但需鉴权）"
curl -sS -m 15 -o /dev/null -w '  HTTP=%{http_code}  对端=%{remote_ip}\n' "https://${BUCKET}.cos.${REGION}.myqcloud.com/" 2>&1
hr

echo "【7】MTU/分片探测（大包能否通过，排除 MSS 问题）"
for sz in 1200 1400 1472; do
  printf '  payload=%-5s ' "$sz"
  if ping -c 2 -W 3 -s $sz -M do "cos.${REGION}.myqcloud.com" >/dev/null 2>&1; then
    echo "通过"
  else
    echo "不通或不可测（ICMP 可能被禁，参考价值有限）"
  fi
done
hr

cat <<'EOT'
判读要点
  1. 【3】读到 "dec cos probe from local mac" 且 HTTP=200
     → 该机到广州 COS 完全正常，之前的问题不在网络/IP。
  2. 【3】HTTP=403 且 Code=AccessDenied
     → 签名未生效（时钟偏差或 URL 被截断），检查 date -u 与服务端偏差。
  3. 【3】超时 / 连接重置 / 返回 HTML
     → 真被网络层拦了，重点查出口 NAT、安全组、运营商侧。
  4. 【2】个别节点不通
     → 多出口 NAT 负载不均，会表现为"时好时坏"。
  5. 【5】桶清单为空但【4】成功
     → AK 无 ListAllMyBuckets 权限，不影响对象读写。
EOT
