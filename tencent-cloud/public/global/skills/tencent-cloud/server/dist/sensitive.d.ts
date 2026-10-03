/** 敏感操作清单 — 不暴露为 tool，调用时返回明确拒绝 */
export declare const BLOCKED_OPERATIONS: {
    readonly cvm: readonly ["TerminateInstances — 销毁 CVM 实例", "ResetInstancesPassword — 重置实例密码", "ModifySecurityGroupPolicies — 修改安全组规则（含开放 0.0.0.0/0）", "DeleteSecurityGroup — 删除安全组", "AssociateSecurityGroups — 批量绑定危险安全组"];
    readonly lighthouse: readonly ["TerminateInstances — 销毁 Lighthouse 实例", "ResetInstancesPassword — 重置实例密码", "CreateFirewallRules / ModifyFirewallRules — 开放 0.0.0.0/0 全端口", "DeleteFirewallRules — 批量删除防火墙规则（请用控制台）"];
    readonly cos: readonly ["deleteBucket — 删除存储桶", "deleteMultipleObject — 批量删除对象", "deleteBucketPolicy / deleteBucketCors — 删除桶级配置（请用 put 覆盖）"];
    readonly cdb: readonly ["DeleteDatabase — 删除数据库", "DropDatabase / DropTables — 通过 API 删除", "SQL: DROP DATABASE, DROP TABLE, TRUNCATE, DELETE 无 WHERE"];
    readonly dns: readonly ["DeleteRecordBatch — 批量删除解析记录"];
    readonly pages: readonly ["DeletePagesProject — 删除 Pages/Makers 项目", "DeletePagesProjectCustomDomains — 摘掉线上自定义域名"];
    readonly teo: readonly ["DeleteZone / ModifyZoneStatus — 停用或删除 EdgeOne 站点", "ModifyHostsCertificate Mode=disable — 关闭线上域名证书（需 confirm=true 才可调用）"];
};
export declare function rejectBlocked(namespace: string, operation: string, reason?: string): {
    blocked: boolean;
    namespace: string;
    operation: string;
    message: string;
    hint: string;
};
/** 检测防火墙/安全组规则是否开放 0.0.0.0/0 危险端口 */
export declare function isDangerousNetworkRule(rule: {
    CidrBlock?: string;
    Cidr?: string;
    Protocol?: string;
    Port?: string;
    Action?: string;
}): boolean;
export declare function assertSafeFirewallRules(rules: Array<{
    CidrBlock?: string;
    Cidr?: string;
    Protocol?: string;
    Port?: string;
    Action?: string;
}>): void;
/** 校验单条入站规则：禁止全开放 CIDR、全端口/全协议 */
export declare function assertSafeIngressRule(rule: {
    cidr: string;
    port: string | number;
    protocol?: string;
}): void;
export declare function normalizeCidr(cidr: string): string;
export declare function validateSqlStatement(sql: string): {
    allowed: boolean;
    reason?: string;
};
