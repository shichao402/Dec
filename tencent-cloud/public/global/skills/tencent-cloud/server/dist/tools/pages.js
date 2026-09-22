import { readFileSync, readdirSync, statSync } from "node:fs";
import { basename, join, posix, relative, resolve, sep } from "node:path";
import { z } from "zod";
import { createTempCosClient } from "../clients.js";
import { promisifyCos, resolvePagesCredentials } from "../lib.js";
import { rejectBlocked } from "../sensitive.js";
import { safeResult, textResult } from "./common.js";
const MAX_UPLOAD_FILES = 2000;
const MAX_UPLOAD_BYTES = 64 * 1024 * 1024;
const CONTENT_TYPES = {
    ".html": "text/html; charset=utf-8",
    ".htm": "text/html; charset=utf-8",
    ".css": "text/css; charset=utf-8",
    ".js": "application/javascript; charset=utf-8",
    ".mjs": "application/javascript; charset=utf-8",
    ".json": "application/json; charset=utf-8",
    ".txt": "text/plain; charset=utf-8",
    ".xml": "application/xml; charset=utf-8",
    ".svg": "image/svg+xml",
    ".png": "image/png",
    ".jpg": "image/jpeg",
    ".jpeg": "image/jpeg",
    ".gif": "image/gif",
    ".webp": "image/webp",
    ".ico": "image/x-icon",
    ".woff": "font/woff",
    ".woff2": "font/woff2",
    ".zip": "application/zip"
};
function contentTypeFor(name) {
    const dot = name.lastIndexOf(".");
    if (dot < 0)
        return "application/octet-stream";
    return CONTENT_TYPES[name.slice(dot).toLowerCase()] ?? "application/octet-stream";
}
async function callPages(projectRoot, action, payload, region) {
    const creds = resolvePagesCredentials(projectRoot, region);
    const response = await fetch(creds.baseUrl, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
            Authorization: `Bearer ${creds.token}`
        },
        body: JSON.stringify({ Action: action, ...payload })
    });
    const raw = await response.text();
    let parsed;
    try {
        parsed = raw ? JSON.parse(raw) : {};
    }
    catch {
        throw new Error(`Pages ${action} 返回非 JSON (HTTP ${response.status}): ${raw.slice(0, 400)}`);
    }
    const body = parsed.Response ?? parsed;
    const error = body.Error;
    if (error) {
        throw new Error(`Pages ${action} 失败: ${error.Code ?? "Unknown"} ${error.Message ?? ""}`.trim());
    }
    if (!response.ok) {
        throw new Error(`Pages ${action} 失败 (HTTP ${response.status}): ${raw.slice(0, 400)}`);
    }
    return body;
}
function walkFiles(root) {
    const out = [];
    const visit = (dir) => {
        for (const entry of readdirSync(dir, { withFileTypes: true })) {
            if (entry.name === ".git" || entry.name === "node_modules")
                continue;
            const full = join(dir, entry.name);
            if (entry.isDirectory())
                visit(full);
            else if (entry.isFile())
                out.push(full);
        }
    };
    visit(root);
    return out;
}
export function registerPagesTools(server, ctx) {
    const call = (action, payload, region) => callPages(ctx.projectRoot, action, payload, region);
    server.registerTool("pages_describe_projects", {
        description: "查询 EdgeOne Pages/Makers 项目列表（DescribePagesProjects）",
        inputSchema: {
            name: z.string().optional().describe("按项目名过滤"),
            status: z.string().optional().describe("按状态过滤，如 Normal"),
            projectIds: z.array(z.string()).optional(),
            limit: z.number().int().min(1).max(100).optional(),
            offset: z.number().int().min(0).optional(),
            region: z.string().optional().describe("china（默认）或 global")
        }
    }, async ({ name, status, projectIds, limit, offset, region }) => {
        const filters = [];
        if (name)
            filters.push({ Name: "Name", Values: [name] });
        if (status)
            filters.push({ Name: "Status", Values: [status] });
        const payload = {
            Limit: limit ?? 20,
            Offset: offset ?? 0
        };
        if (filters.length > 0)
            payload.Filters = filters;
        if (projectIds?.length)
            payload.ProjectIds = projectIds;
        return safeResult(await call("DescribePagesProjects", payload, region));
    });
    server.registerTool("pages_create_project", {
        description: "创建 Pages/Makers 项目（CreatePagesProject）。默认 Provider=Upload，即由本地产物直接部署，不接 Git",
        inputSchema: {
            name: z
                .string()
                .describe("项目名：小写字母/数字/连字符，长度 5-63，不能含 edgeone 后缀，账号内唯一"),
            provider: z.enum(["Upload", "Github", "Gitee", "Gitlab", "Coding"]).optional(),
            area: z.enum(["mainland", "overseas", "global"]).optional(),
            outputDir: z.string().optional().describe("构建产物目录；纯静态可不填"),
            buildCmd: z.string().optional(),
            installCmd: z.string().optional(),
            repoUrl: z.string().optional(),
            repoBranch: z.string().optional(),
            region: z.string().optional()
        }
    }, async ({ name, provider, area, outputDir, buildCmd, installCmd, repoUrl, repoBranch, region }) => {
        if (!/^[a-z0-9-]{5,63}$/.test(name) || name.startsWith("-") || name.endsWith("-")) {
            throw new Error("项目名不合规：仅小写字母/数字/连字符，长度 5-63，不能以连字符开头或结尾");
        }
        const payload = {
            Name: name,
            Channel: "Custom",
            Provider: provider ?? "Upload",
            Source: "ZP"
        };
        if (area)
            payload.Area = area;
        if (outputDir)
            payload.OutputDir = outputDir;
        if (buildCmd)
            payload.BuildCmd = buildCmd;
        if (installCmd)
            payload.InstallCmd = installCmd;
        if (repoUrl)
            payload.RepoUrl = repoUrl;
        if (repoBranch)
            payload.RepoBranch = repoBranch;
        return safeResult(await call("CreatePagesProject", payload, region));
    });
    server.registerTool("pages_describe_cos_temp_token", {
        description: "取上传产物用的 COS 临时密钥与目标路径（DescribePagesCosTempToken）。一般直接用 pages_deploy_dir",
        inputSchema: {
            projectId: z.string(),
            region: z.string().optional()
        }
    }, async ({ projectId, region }) => {
        return safeResult(await call("DescribePagesCosTempToken", { ProjectId: projectId }, region));
    });
    server.registerTool("pages_create_deployment", {
        description: "创建部署任务（CreatePagesDeployment）。产物已经在 COS 上时才用这个，否则用 pages_deploy_dir",
        inputSchema: {
            projectId: z.string(),
            tempBucketPath: z
                .string()
                .describe("来自 pages_describe_cos_temp_token；Folder 传目录，Zip 传完整文件路径"),
            distType: z.enum(["Folder", "Zip"]).optional(),
            env: z.enum(["Production", "Preview"]).optional(),
            region: z.string().optional()
        }
    }, async ({ projectId, tempBucketPath, distType, env, region }) => {
        return safeResult(await call("CreatePagesDeployment", {
            ProjectId: projectId,
            ViaMeta: "Upload",
            Provider: "Upload",
            Env: env ?? "Production",
            DistType: distType ?? "Folder",
            TempBucketPath: tempBucketPath
        }, region));
    });
    server.registerTool("pages_deploy_dir", {
        description: "把本地目录或 zip 部署到 Pages/Makers：取临时凭据 → 上传 COS → 创建部署。需 confirm=true",
        inputSchema: {
            projectId: z.string(),
            path: z.string().describe("本地目录，或单个 .zip 文件的路径"),
            confirm: z.boolean().describe("必须为 true 才真正上传并部署"),
            env: z.enum(["Production", "Preview"]).optional(),
            region: z.string().optional()
        }
    }, async ({ projectId, path, confirm, env, region }) => {
        if (!confirm) {
            return textResult(rejectBlocked("pages", "deploy_dir", "上传并部署需显式设置 confirm=true"));
        }
        const source = resolve(path);
        const info = statSync(source);
        const asZip = info.isFile();
        if (asZip && !source.toLowerCase().endsWith(".zip")) {
            throw new Error("单文件部署只接受 .zip；其他情况请传目录");
        }
        const files = asZip ? [source] : walkFiles(source);
        if (files.length === 0) {
            throw new Error(`${source} 下没有文件可部署`);
        }
        if (files.length > MAX_UPLOAD_FILES) {
            throw new Error(`文件数 ${files.length} 超过上限 ${MAX_UPLOAD_FILES}`);
        }
        const totalBytes = files.reduce((sum, file) => sum + statSync(file).size, 0);
        if (totalBytes > MAX_UPLOAD_BYTES) {
            throw new Error(`总大小 ${totalBytes} 字节超过上限 ${MAX_UPLOAD_BYTES}`);
        }
        const temp = await call("DescribePagesCosTempToken", { ProjectId: projectId }, region);
        if (!temp.Credentials?.TmpSecretId) {
            throw new Error("DescribePagesCosTempToken 未返回临时密钥");
        }
        const cos = createTempCosClient({
            tmpSecretId: temp.Credentials.TmpSecretId,
            tmpSecretKey: temp.Credentials.TmpSecretKey,
            token: temp.Credentials.Token
        });
        const uploaded = [];
        for (const file of files) {
            const rel = asZip ? basename(file) : relative(source, file).split(sep).join(posix.sep);
            const key = `${temp.TargetPath}/${rel}`;
            await promisifyCos(cos, "putObject", {
                Bucket: temp.Bucket,
                Region: temp.Region,
                Key: key,
                Body: readFileSync(file),
                ContentType: contentTypeFor(rel)
            });
            uploaded.push(rel);
        }
        const tempBucketPath = asZip
            ? `${temp.TargetPath}/${basename(source)}`
            : temp.TargetPath;
        const deployment = await call("CreatePagesDeployment", {
            ProjectId: projectId,
            ViaMeta: "Upload",
            Provider: "Upload",
            Env: env ?? "Production",
            DistType: asZip ? "Zip" : "Folder",
            TempBucketPath: tempBucketPath
        }, region);
        return safeResult({
            projectId,
            env: env ?? "Production",
            distType: asZip ? "Zip" : "Folder",
            uploadedCount: uploaded.length,
            uploadedBytes: totalBytes,
            uploaded: uploaded.slice(0, 50),
            tempBucketPath,
            deployment,
            hint: "用 pages_describe_deployments 看构建状态，失败时 pages_describe_deployment_log 取日志"
        });
    });
    server.registerTool("pages_describe_deployments", {
        description: "查询项目部署列表（DescribePagesDeployments）",
        inputSchema: {
            projectId: z.string(),
            status: z.string().optional().describe("如 Success / Failed / Process"),
            deploymentIds: z.array(z.string()).optional(),
            limit: z.number().int().min(1).max(100).optional(),
            offset: z.number().int().min(0).optional(),
            region: z.string().optional()
        }
    }, async ({ projectId, status, deploymentIds, limit, offset, region }) => {
        const payload = {
            ProjectId: projectId,
            Limit: limit ?? 20,
            Offset: offset ?? 0
        };
        if (status)
            payload.Filters = [{ Name: "Status", Values: [status] }];
        if (deploymentIds?.length)
            payload.DeploymentIds = deploymentIds;
        return safeResult(await call("DescribePagesDeployments", payload, region));
    });
    server.registerTool("pages_describe_deployment_log", {
        description: "取某次部署的构建日志地址（DescribePagesDeploymentLog）",
        inputSchema: {
            projectId: z.string(),
            deploymentId: z.string(),
            region: z.string().optional()
        }
    }, async ({ projectId, deploymentId, region }) => {
        return safeResult(await call("DescribePagesDeploymentLog", { ProjectId: projectId, DeploymentId: deploymentId }, region));
    });
    server.registerTool("pages_describe_project_envs", {
        description: "查询项目环境变量（DescribePagesProjectEnvs）",
        inputSchema: {
            projectId: z.string(),
            region: z.string().optional()
        }
    }, async ({ projectId, region }) => {
        return safeResult(await call("DescribePagesProjectEnvs", { ProjectId: projectId }, region));
    });
    server.registerTool("pages_modify_project_envs", {
        description: "新增或更新项目环境变量（ModifyPagesProjectEnvs）。重复 Key 会被服务端拒绝",
        inputSchema: {
            projectId: z.string(),
            envVars: z
                .array(z.object({
                key: z.string().min(1).max(255),
                value: z.string().min(1).max(1000),
                comment: z.string().max(255).optional(),
                id: z.number().int().optional().describe("带 id 表示更新已有记录")
            }))
                .min(1),
            region: z.string().optional()
        }
    }, async ({ projectId, envVars, region }) => {
        const EnvVars = envVars.map((item) => {
            const entry = { Key: item.key, Value: item.value };
            if (item.comment)
                entry.Comment = item.comment;
            if (item.id !== undefined)
                entry.Id = item.id;
            return entry;
        });
        return safeResult(await call("ModifyPagesProjectEnvs", { ProjectId: projectId, EnvVars }, region));
    });
    server.registerTool("pages_create_zone_custom_domain", {
        description: "给项目绑自定义域名（CreatePagesZoneCustomDomain）。返回 Cname 与归属校验要求，之后用 dns_create_record 指过去",
        inputSchema: {
            projectId: z.string(),
            domain: z.string().describe("完整主机名，如 updates.firoyang.com"),
            region: z.string().optional()
        }
    }, async ({ projectId, domain, region }) => {
        const name = domain.trim().toLowerCase();
        if (!name || name.includes("/") || name.includes(" ")) {
            throw new Error("domain 必须是完整主机名，不能带路径");
        }
        return safeResult(await call("CreatePagesZoneCustomDomain", { ProjectId: projectId, Domain: name }, region));
    });
    server.registerTool("pages_describe_zone_custom_domains", {
        description: "查询项目已绑定的自定义域名（DescribePagesZoneCustomDomains）",
        inputSchema: {
            projectId: z.string(),
            domain: z.string().optional(),
            region: z.string().optional()
        }
    }, async ({ projectId, domain, region }) => {
        const payload = { ProjectId: projectId };
        if (domain)
            payload.Domain = domain.trim().toLowerCase();
        return safeResult(await call("DescribePagesZoneCustomDomains", payload, region));
    });
}
//# sourceMappingURL=pages.js.map