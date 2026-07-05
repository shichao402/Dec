import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { createCosClient } from "../clients.js";
import { cosBucketOrThrow, promisifyCos } from "../lib.js";
import { rejectBlocked } from "../sensitive.js";
import { safeResult, textResult, type ToolContext } from "./common.js";

export function registerCosTools(server: McpServer, ctx: ToolContext) {
  server.registerTool(
    "cos.list_objects",
    {
      description: "列出 COS 存储桶对象（getBucket）",
      inputSchema: {
        bucket: z.string().optional(),
        region: z.string().optional(),
        prefix: z.string().optional(),
        maxKeys: z.number().int().min(1).max(1000).optional()
      }
    },
    async ({ bucket, region, prefix, maxKeys }) => {
      const creds = ctx.getCredentials();
      const cos = createCosClient(creds);
      const result = await promisifyCos(cos, "getBucket", {
        Bucket: cosBucketOrThrow(creds, bucket),
        Region: region ?? creds.cosRegion,
        Prefix: prefix,
        MaxKeys: maxKeys ?? 100
      });
      return textResult(result);
    }
  );

  server.registerTool(
    "cos.upload_object",
    {
      description: "上传文本内容到 COS（putObject）",
      inputSchema: {
        key: z.string(),
        body: z.string(),
        bucket: z.string().optional(),
        region: z.string().optional(),
        contentType: z.string().optional()
      }
    },
    async ({ key, body, bucket, region, contentType }) => {
      const creds = ctx.getCredentials();
      const cos = createCosClient(creds);
      const result = await promisifyCos(cos, "putObject", {
        Bucket: cosBucketOrThrow(creds, bucket),
        Region: region ?? creds.cosRegion,
        Key: key,
        Body: body,
        ContentType: contentType ?? "text/plain; charset=utf-8"
      });
      return textResult(result);
    }
  );

  server.registerTool(
    "cos.download_object",
    {
      description: "下载 COS 对象为文本（getObject，小于 4MB 建议；大二进制请用预签名 URL）",
      inputSchema: {
        key: z.string(),
        bucket: z.string().optional(),
        region: z.string().optional()
      }
    },
    async ({ key, bucket, region }) => {
      const creds = ctx.getCredentials();
      const cos = createCosClient(creds);
      const result = (await promisifyCos<{ Body?: Buffer | string }>(cos, "getObject", {
        Bucket: cosBucketOrThrow(creds, bucket),
        Region: region ?? creds.cosRegion,
        Key: key
      })) as { Body?: Buffer | string; [k: string]: unknown };
      const body = result.Body;
      const text =
        body instanceof Buffer ? body.toString("utf8") : typeof body === "string" ? body : "";
      return textResult({ ...result, Body: text, bodyLength: text.length });
    }
  );

  server.registerTool(
    "cos.head_object",
    {
      description: "查询对象元数据（headObject）",
      inputSchema: {
        key: z.string(),
        bucket: z.string().optional(),
        region: z.string().optional()
      }
    },
    async ({ key, bucket, region }) => {
      const creds = ctx.getCredentials();
      const cos = createCosClient(creds);
      const result = await promisifyCos(cos, "headObject", {
        Bucket: cosBucketOrThrow(creds, bucket),
        Region: region ?? creds.cosRegion,
        Key: key
      });
      return textResult(result);
    }
  );

  server.registerTool(
    "cos.delete_object",
    {
      description: "删除单个 COS 对象（需 confirm=true）",
      inputSchema: {
        key: z.string(),
        confirm: z.boolean().describe("必须为 true 才执行删除"),
        bucket: z.string().optional(),
        region: z.string().optional()
      }
    },
    async ({ key, confirm, bucket, region }) => {
      if (!confirm) {
        return textResult(
          rejectBlocked("cos", "delete_object", "删除对象需显式设置 confirm=true")
        );
      }
      const creds = ctx.getCredentials();
      const cos = createCosClient(creds);
      const result = await promisifyCos(cos, "deleteObject", {
        Bucket: cosBucketOrThrow(creds, bucket),
        Region: region ?? creds.cosRegion,
        Key: key
      });
      return textResult(result);
    }
  );

  server.registerTool(
    "cos.get_bucket_info",
    {
      description: "查询存储桶基本信息（headBucket）",
      inputSchema: {
        bucket: z.string().optional(),
        region: z.string().optional()
      }
    },
    async ({ bucket, region }) => {
      const creds = ctx.getCredentials();
      const cos = createCosClient(creds);
      const result = await promisifyCos(cos, "headBucket", {
        Bucket: cosBucketOrThrow(creds, bucket),
        Region: region ?? creds.cosRegion
      });
      return textResult(result);
    }
  );

  server.registerTool(
    "cos.get_bucket_cors",
    {
      description: "获取存储桶 CORS 配置",
      inputSchema: {
        bucket: z.string().optional(),
        region: z.string().optional()
      }
    },
    async ({ bucket, region }) => {
      const creds = ctx.getCredentials();
      const cos = createCosClient(creds);
      const result = await promisifyCos(cos, "getBucketCors", {
        Bucket: cosBucketOrThrow(creds, bucket),
        Region: region ?? creds.cosRegion
      });
      return textResult(result);
    }
  );

  server.registerTool(
    "cos.put_bucket_cors",
    {
      description: "设置存储桶 CORS 配置（CORSRules 为 JSON 数组字符串）",
      inputSchema: {
        corsRulesJson: z
          .string()
          .describe('CORS 规则 JSON，如 [{"AllowedOrigin":["*"],"AllowedMethod":["GET"],"AllowedHeader":["*"],"ExposeHeader":[],"MaxAgeSeconds":600}]'),
        bucket: z.string().optional(),
        region: z.string().optional()
      }
    },
    async ({ corsRulesJson, bucket, region }) => {
      const creds = ctx.getCredentials();
      const cos = createCosClient(creds);
      const CORSRules = JSON.parse(corsRulesJson) as unknown;
      const result = await promisifyCos(cos, "putBucketCors", {
        Bucket: cosBucketOrThrow(creds, bucket),
        Region: region ?? creds.cosRegion,
        CORSConfiguration: { CORSRules }
      });
      return textResult(result);
    }
  );

  server.registerTool(
    "cos.get_bucket_policy",
    {
      description: "获取存储桶 Policy",
      inputSchema: {
        bucket: z.string().optional(),
        region: z.string().optional()
      }
    },
    async ({ bucket, region }) => {
      const creds = ctx.getCredentials();
      const cos = createCosClient(creds);
      const result = await promisifyCos(cos, "getBucketPolicy", {
        Bucket: cosBucketOrThrow(creds, bucket),
        Region: region ?? creds.cosRegion
      });
      return textResult(result);
    }
  );

  server.registerTool(
    "cos.put_bucket_policy",
    {
      description: "设置存储桶 Policy（policyJson 为 JSON 字符串）",
      inputSchema: {
        policyJson: z.string().describe("Bucket Policy JSON 文档"),
        bucket: z.string().optional(),
        region: z.string().optional()
      }
    },
    async ({ policyJson, bucket, region }) => {
      const creds = ctx.getCredentials();
      const cos = createCosClient(creds);
      JSON.parse(policyJson);
      const result = await promisifyCos(cos, "putBucketPolicy", {
        Bucket: cosBucketOrThrow(creds, bucket),
        Region: region ?? creds.cosRegion,
        Policy: policyJson
      });
      return textResult(result);
    }
  );
}
