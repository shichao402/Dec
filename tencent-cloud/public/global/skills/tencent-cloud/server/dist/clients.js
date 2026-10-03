import tencentcloud from "tencentcloud-sdk-nodejs";
import COS from "cos-nodejs-sdk-v5";
const CvmClient = tencentcloud.cvm.v20170312.Client;
const DnspodClient = tencentcloud.dnspod.v20210323.Client;
const LighthouseClient = tencentcloud.lighthouse.v20200324.Client;
const VpcClient = tencentcloud.vpc.v20170312.Client;
const TatClient = tencentcloud.tat.v20201028.Client;
const CdbClient = tencentcloud.cdb.v20170320.Client;
const SslClient = tencentcloud.ssl.v20191205.Client;
const TeoClient = tencentcloud.teo.v20220901.Client;
function sdkCredential(creds) {
    return {
        secretId: creds.secretId,
        secretKey: creds.secretKey
    };
}
function clientConfig(creds, region, endpoint) {
    return {
        credential: sdkCredential(creds),
        region,
        profile: { httpProfile: { endpoint } }
    };
}
export function createCvmClient(creds, region) {
    return new CvmClient(clientConfig(creds, region ?? creds.region, "cvm.tencentcloudapi.com"));
}
export function createDnspodClient(creds) {
    return new DnspodClient({
        credential: sdkCredential(creds),
        region: "",
        profile: { httpProfile: { endpoint: "dnspod.tencentcloudapi.com" } }
    });
}
export function createLighthouseClient(creds, region) {
    return new LighthouseClient(clientConfig(creds, region ?? creds.region, "lighthouse.tencentcloudapi.com"));
}
export function createVpcClient(creds, region) {
    return new VpcClient(clientConfig(creds, region ?? creds.region, "vpc.tencentcloudapi.com"));
}
export function createTatClient(creds, region) {
    return new TatClient(clientConfig(creds, region ?? creds.region, "tat.tencentcloudapi.com"));
}
export function createCdbClient(creds, region) {
    return new CdbClient(clientConfig(creds, region ?? creds.cdbRegion, "cdb.tencentcloudapi.com"));
}
export function createSslClient(creds, region) {
    return new SslClient(clientConfig(creds, region ?? creds.region, "ssl.tencentcloudapi.com"));
}
export function createTeoClient(creds, region) {
    return new TeoClient(clientConfig(creds, region ?? creds.region, "teo.tencentcloudapi.com"));
}
export function createCosClient(creds) {
    return new COS({
        SecretId: creds.secretId,
        SecretKey: creds.secretKey
    });
}
/** COS client for STS credentials, e.g. the upload token Pages hands out. */
export function createTempCosClient(temp) {
    return new COS({
        SecretId: temp.tmpSecretId,
        SecretKey: temp.tmpSecretKey,
        SecurityToken: temp.token
    });
}
//# sourceMappingURL=clients.js.map