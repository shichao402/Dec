export async function runShellCommand(client, instanceIds, script, options) {
    const content = Buffer.from(script, "utf8").toString("base64");
    return client.RunCommand({
        InstanceIds: instanceIds,
        Content: content,
        CommandType: "SHELL",
        CommandName: options?.commandName ?? "tencent-cloud-mcp",
        Timeout: options?.timeout ?? 120,
        SaveCommand: false
    });
}
export async function describeInvocation(client, invocationId, limit = 20) {
    return client.DescribeInvocationTasks({
        Filters: [{ Name: "invocation-id", Values: [invocationId] }],
        Limit: limit,
        Offset: 0
    });
}
/** 通过 TAT 上传文本文件（base64 写入） */
export function buildUploadScript(remotePath, base64Content) {
    const dir = remotePath.includes("/") ? remotePath.slice(0, remotePath.lastIndexOf("/")) : ".";
    return `set -e
mkdir -p "${dir}"
echo '${base64Content}' | base64 -d > "${remotePath}"
echo "OK: wrote ${remotePath}"`;
}
/** 通过 TAT 下载文件（cat + base64 输出） */
export function buildDownloadScript(remotePath) {
    return `set -e
if [ ! -f "${remotePath}" ]; then echo "ERR: file not found" >&2; exit 1; fi
base64 "${remotePath}"`;
}
//# sourceMappingURL=tat.js.map