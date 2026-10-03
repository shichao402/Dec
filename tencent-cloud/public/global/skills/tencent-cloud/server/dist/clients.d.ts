import COS from "cos-nodejs-sdk-v5";
import type { TencentCredentials } from "./lib.js";
export declare function createCvmClient(creds: TencentCredentials, region?: string): import("tencentcloud-sdk-nodejs/tencentcloud/services/cvm/v20170312/cvm_client.js").Client;
export declare function createDnspodClient(creds: TencentCredentials): import("tencentcloud-sdk-nodejs/tencentcloud/services/dnspod/v20210323/dnspod_client.js").Client;
export declare function createLighthouseClient(creds: TencentCredentials, region?: string): import("tencentcloud-sdk-nodejs/tencentcloud/services/lighthouse/v20200324/lighthouse_client.js").Client;
export declare function createVpcClient(creds: TencentCredentials, region?: string): import("tencentcloud-sdk-nodejs/tencentcloud/services/vpc/v20170312/vpc_client.js").Client;
export declare function createTatClient(creds: TencentCredentials, region?: string): import("tencentcloud-sdk-nodejs/tencentcloud/services/tat/v20201028/tat_client.js").Client;
export declare function createCdbClient(creds: TencentCredentials, region?: string): import("tencentcloud-sdk-nodejs/tencentcloud/services/cdb/v20170320/cdb_client.js").Client;
export declare function createSslClient(creds: TencentCredentials, region?: string): import("tencentcloud-sdk-nodejs/tencentcloud/services/ssl/v20191205/ssl_client.js").Client;
export declare function createTeoClient(creds: TencentCredentials, region?: string): import("tencentcloud-sdk-nodejs/tencentcloud/services/teo/v20220901/teo_client.js").Client;
export declare function createCosClient(creds: TencentCredentials): COS;
/** COS client for STS credentials, e.g. the upload token Pages hands out. */
export declare function createTempCosClient(temp: {
    tmpSecretId: string;
    tmpSecretKey: string;
    token: string;
}): COS;
