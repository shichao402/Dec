import type COS from "cos-nodejs-sdk-v5";
export declare function resolveProjectRoot(): string;
export declare const MISE_CONF_D_REL = ".config/mise/conf.d/tencent-cloud.toml";
export declare const MISE_LOCAL_REL = "mise.local.toml";
export declare function resolveMiseSecretsPath(projectRoot: string): string;
export declare function defaultMiseSecretsWritePath(projectRoot: string): string;
export declare function loadEnvFromMiseLocal(projectRoot: string): Record<string, string>;
export type TencentCredentials = {
    secretId: string;
    secretKey: string;
    region: string;
    cosBucket: string;
    cosRegion: string;
    cdbRegion: string;
    cdbInstanceId: string;
};
export type CdbConnectionConfig = {
    host: string;
    port: number;
    user: string;
    password: string;
    database: string;
};
export declare function resolveCredentials(projectRoot: string): TencentCredentials;
export declare function resolveEnv(projectRoot: string): Record<string, string>;
export type PagesCredentials = {
    token: string;
    baseUrl: string;
    region: "china" | "global";
};
/**
 * EdgeOne Pages / Makers authenticates with a console-issued bearer token on a
 * separate endpoint. The CAM SecretId/Key pair cannot sign these calls, so the
 * token is resolved independently of resolveCredentials.
 */
export declare function resolvePagesCredentials(projectRoot: string, region?: string): PagesCredentials;
export declare function syncPkvConfig(projectRoot: string, folder?: string): {
    source: string;
    path: string;
    keys: string[];
};
export declare function syncDecAssets(projectRoot: string): string;
export declare const TOOL_NAMESPACES: {
    readonly meta: {
        readonly description: "元信息、密钥同步、敏感操作说明";
        readonly tools: readonly ["meta_list_namespaces", "meta_list_blocked_operations", "meta_sync_pkv_config", "meta_sync_dec_assets"];
    };
    readonly cvm: {
        readonly description: "云服务器 CVM（查询、TAT 命令与文件传输）";
        readonly tools: readonly ["cvm_describe_instances", "cvm_describe_security_groups", "cvm_run_command", "cvm_upload_file", "cvm_download_file", "cvm_describe_invocation"];
    };
    readonly cos: {
        readonly description: "对象存储 COS（对象与桶配置）";
        readonly tools: readonly ["cos_list_objects", "cos_upload_object", "cos_download_object", "cos_head_object", "cos_delete_object", "cos_get_bucket_info", "cos_get_bucket_cors", "cos_put_bucket_cors", "cos_get_bucket_policy", "cos_put_bucket_policy", "cos_get_bucket_domain", "cos_put_bucket_domain"];
    };
    readonly dns: {
        readonly description: "DNSPod 域名解析";
        readonly tools: readonly ["dns_describe_domains", "dns_describe_records", "dns_create_record", "dns_modify_record", "dns_delete_record"];
    };
    readonly ssl: {
        readonly description: "SSL 证书申请与部署到 COS 自定义域名";
        readonly tools: readonly ["ssl_describe_certificates", "ssl_describe_certificate_detail", "ssl_apply_certificate", "ssl_describe_host_cos_instances", "ssl_deploy_certificate_instance", "ssl_describe_host_deploy_record_detail"];
    };
    readonly pages: {
        readonly description: "EdgeOne Pages / Makers 静态站（独立 API Token，非 CAM 密钥）";
        readonly tools: readonly ["pages_describe_projects", "pages_create_project", "pages_describe_cos_temp_token", "pages_create_deployment", "pages_deploy_dir", "pages_describe_deployments", "pages_describe_deployment_log", "pages_describe_project_envs", "pages_modify_project_envs", "pages_create_zone_custom_domain", "pages_describe_zone_custom_domains"];
    };
    readonly teo: {
        readonly description: "EdgeOne 站点侧（标准 TC3 密钥）：域名归属、CNAME、免费证书";
        readonly tools: readonly ["teo_describe_zones", "teo_verify_ownership", "teo_check_cname_status", "teo_apply_free_certificate", "teo_check_free_certificate_verification", "teo_modify_hosts_certificate"];
    };
    readonly lighthouse: {
        readonly description: "轻量应用服务器 Lighthouse";
        readonly tools: readonly ["lighthouse_describe_instances", "lighthouse_describe_firewall_rules", "lighthouse_run_command", "lighthouse_upload_file", "lighthouse_download_file", "lighthouse_describe_invocation"];
    };
    readonly cdb: {
        readonly description: "云数据库 MySQL（CDB 管控 API + 可选 SQL）";
        readonly tools: readonly ["cdb_describe_instances", "cdb_describe_wan_service", "cdb_open_wan_service", "cdb_close_wan_service", "cdb_describe_databases", "cdb_describe_accounts", "cdb_describe_tables", "cdb_create_database", "cdb_describe_security_groups", "cdb_add_security_group_ingress", "cdb_query_sql", "cdb_execute_sql"];
    };
};
export declare function listNamespaces(): {
    namespace: string;
    description: "元信息、密钥同步、敏感操作说明" | "云服务器 CVM（查询、TAT 命令与文件传输）" | "对象存储 COS（对象与桶配置）" | "DNSPod 域名解析" | "SSL 证书申请与部署到 COS 自定义域名" | "EdgeOne Pages / Makers 静态站（独立 API Token，非 CAM 密钥）" | "EdgeOne 站点侧（标准 TC3 密钥）：域名归属、CNAME、免费证书" | "轻量应用服务器 Lighthouse" | "云数据库 MySQL（CDB 管控 API + 可选 SQL）";
    tools: ("meta_list_namespaces" | "meta_list_blocked_operations" | "meta_sync_pkv_config" | "meta_sync_dec_assets" | "cvm_describe_instances" | "cvm_describe_security_groups" | "cvm_run_command" | "cvm_upload_file" | "cvm_download_file" | "cvm_describe_invocation" | "cos_list_objects" | "cos_upload_object" | "cos_download_object" | "cos_head_object" | "cos_delete_object" | "cos_get_bucket_info" | "cos_get_bucket_cors" | "cos_put_bucket_cors" | "cos_get_bucket_policy" | "cos_put_bucket_policy" | "cos_get_bucket_domain" | "cos_put_bucket_domain" | "dns_describe_domains" | "dns_describe_records" | "dns_create_record" | "dns_modify_record" | "dns_delete_record" | "ssl_describe_certificates" | "ssl_describe_certificate_detail" | "ssl_apply_certificate" | "ssl_describe_host_cos_instances" | "ssl_deploy_certificate_instance" | "ssl_describe_host_deploy_record_detail" | "pages_describe_projects" | "pages_create_project" | "pages_describe_cos_temp_token" | "pages_create_deployment" | "pages_deploy_dir" | "pages_describe_deployments" | "pages_describe_deployment_log" | "pages_describe_project_envs" | "pages_modify_project_envs" | "pages_create_zone_custom_domain" | "pages_describe_zone_custom_domains" | "teo_describe_zones" | "teo_verify_ownership" | "teo_check_cname_status" | "teo_apply_free_certificate" | "teo_check_free_certificate_verification" | "teo_modify_hosts_certificate" | "lighthouse_describe_instances" | "lighthouse_describe_firewall_rules" | "lighthouse_run_command" | "lighthouse_upload_file" | "lighthouse_download_file" | "lighthouse_describe_invocation" | "cdb_describe_instances" | "cdb_describe_wan_service" | "cdb_open_wan_service" | "cdb_close_wan_service" | "cdb_describe_databases" | "cdb_describe_accounts" | "cdb_describe_tables" | "cdb_create_database" | "cdb_describe_security_groups" | "cdb_add_security_group_ingress" | "cdb_query_sql" | "cdb_execute_sql")[];
}[];
export declare function listBlockedOperations(): {
    policy: string;
    blocked: {
        readonly cvm: readonly ["TerminateInstances — 销毁 CVM 实例", "ResetInstancesPassword — 重置实例密码", "ModifySecurityGroupPolicies — 修改安全组规则（含开放 0.0.0.0/0）", "DeleteSecurityGroup — 删除安全组", "AssociateSecurityGroups — 批量绑定危险安全组"];
        readonly lighthouse: readonly ["TerminateInstances — 销毁 Lighthouse 实例", "ResetInstancesPassword — 重置实例密码", "CreateFirewallRules / ModifyFirewallRules — 开放 0.0.0.0/0 全端口", "DeleteFirewallRules — 批量删除防火墙规则（请用控制台）"];
        readonly cos: readonly ["deleteBucket — 删除存储桶", "deleteMultipleObject — 批量删除对象", "deleteBucketPolicy / deleteBucketCors — 删除桶级配置（请用 put 覆盖）"];
        readonly cdb: readonly ["DeleteDatabase — 删除数据库", "DropDatabase / DropTables — 通过 API 删除", "SQL: DROP DATABASE, DROP TABLE, TRUNCATE, DELETE 无 WHERE"];
        readonly dns: readonly ["DeleteRecordBatch — 批量删除解析记录"];
        readonly pages: readonly ["DeletePagesProject — 删除 Pages/Makers 项目", "DeletePagesProjectCustomDomains — 摘掉线上自定义域名"];
        readonly teo: readonly ["DeleteZone / ModifyZoneStatus — 停用或删除 EdgeOne 站点", "ModifyHostsCertificate Mode=disable — 关闭线上域名证书（需 confirm=true 才可调用）"];
    };
    notes: {
        cos_delete_object: string;
        cos_put_bucket_domain: string;
        dns_delete_record: string;
        ssl_apply: string;
        ssl_deploy: string;
        pages_token: string;
        pages_delete: string;
        pages_deploy: string;
        teo_certificate: string;
        cdb_sql: string;
        cdb_close_wan: string;
        cdb_security_group_ingress: string;
        cdb_account_password: string;
        file_transfer: string;
    };
};
export declare function redactSecrets<T>(value: T): T;
export declare function cosBucketOrThrow(creds: TencentCredentials, bucket?: string): string;
export declare function promisifyCos<T>(cos: COS, method: string, params: Record<string, unknown>): Promise<T>;
