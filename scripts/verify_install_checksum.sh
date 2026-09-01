#!/bin/bash
# 验证 install.sh 的摘要解析与校验辅助函数（不执行真实安装）。
set -e

script="$1"
[ -n "${script}" ] || { echo "用法: verify_install_checksum.sh <install.sh 路径>"; exit 2; }

# 只取出待测函数，避免执行 main。
eval "$(sed -n '/^extract_checksum()/,/^}/p' "${script}")"
eval "$(sed -n '/^compute_sha256()/,/^}/p' "${script}")"

json='{"version":"v1.2.3","checksums":{"dec-linux-amd64":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","dec-server-linux-amd64":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}'

got=$(extract_checksum "${json}" "dec-linux-amd64")
[ "${got}" = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ] \
  || { echo "FAIL 精确取值: ${got}"; exit 1; }

# 关键：dec-linux-amd64 是 dec-server-linux-amd64 的子串风险面，必须不串味。
got=$(extract_checksum "${json}" "dec-server-linux-amd64")
[ "${got}" = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" ] \
  || { echo "FAIL 同前缀产物取值: ${got}"; exit 1; }

got=$(extract_checksum "${json}" "dec-mcp-linux-amd64")
[ -z "${got}" ] || { echo "FAIL 缺失应为空: ${got}"; exit 1; }

got=$(extract_checksum '{"version":"v1.2.3"}' "dec-linux-amd64")
[ -z "${got}" ] || { echo "FAIL 无 checksums 段应为空: ${got}"; exit 1; }

# 非法长度摘要不应被接受。
got=$(extract_checksum '{"checksums":{"dec-linux-amd64":"tooshort"}}' "dec-linux-amd64")
[ -z "${got}" ] || { echo "FAIL 非法摘要应为空: ${got}"; exit 1; }

tmp=$(mktemp)
printf 'hello' > "${tmp}"
got=$(compute_sha256 "${tmp}")
want="2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
rm -f "${tmp}"
[ "${got}" = "${want}" ] || { echo "FAIL sha256: ${got}"; exit 1; }

echo "PASS install.sh 摘要逻辑校验通过"
