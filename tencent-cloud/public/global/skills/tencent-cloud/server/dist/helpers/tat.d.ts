import tencentcloud from "tencentcloud-sdk-nodejs";
type TatClient = InstanceType<typeof tencentcloud.tat.v20201028.Client>;
export declare function runShellCommand(client: TatClient, instanceIds: string[], script: string, options?: {
    commandName?: string;
    timeout?: number;
    region?: string;
}): Promise<import("tencentcloud-sdk-nodejs/tencentcloud/services/tat/v20201028/tat_models.js").RunCommandResponse>;
export declare function describeInvocation(client: TatClient, invocationId: string, limit?: number): Promise<import("tencentcloud-sdk-nodejs/tencentcloud/services/tat/v20201028/tat_models.js").DescribeInvocationTasksResponse>;
/** 通过 TAT 上传文本文件（base64 写入） */
export declare function buildUploadScript(remotePath: string, base64Content: string): string;
/** 通过 TAT 下载文件（cat + base64 输出） */
export declare function buildDownloadScript(remotePath: string): string;
export {};
