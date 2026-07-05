import tencentcloud from "tencentcloud-sdk-nodejs";
import COS from "cos-nodejs-sdk-v5";
import type { TencentCredentials } from "./lib.js";

const CvmClient = tencentcloud.cvm.v20170312.Client;
const DnspodClient = tencentcloud.dnspod.v20210323.Client;
const LighthouseClient = tencentcloud.lighthouse.v20200324.Client;
const VpcClient = tencentcloud.vpc.v20170312.Client;
const TatClient = tencentcloud.tat.v20201028.Client;
const CdbClient = tencentcloud.cdb.v20170320.Client;

function sdkCredential(creds: TencentCredentials) {
  return {
    secretId: creds.secretId,
    secretKey: creds.secretKey
  };
}

function clientConfig(creds: TencentCredentials, region: string, endpoint: string) {
  return {
    credential: sdkCredential(creds),
    region,
    profile: { httpProfile: { endpoint } }
  };
}

export function createCvmClient(creds: TencentCredentials, region?: string) {
  return new CvmClient(clientConfig(creds, region ?? creds.region, "cvm.tencentcloudapi.com"));
}

export function createDnspodClient(creds: TencentCredentials) {
  return new DnspodClient({
    credential: sdkCredential(creds),
    region: "",
    profile: { httpProfile: { endpoint: "dnspod.tencentcloudapi.com" } }
  });
}

export function createLighthouseClient(creds: TencentCredentials, region?: string) {
  return new LighthouseClient(
    clientConfig(creds, region ?? creds.region, "lighthouse.tencentcloudapi.com")
  );
}

export function createVpcClient(creds: TencentCredentials, region?: string) {
  return new VpcClient(clientConfig(creds, region ?? creds.region, "vpc.tencentcloudapi.com"));
}

export function createTatClient(creds: TencentCredentials, region?: string) {
  return new TatClient(clientConfig(creds, region ?? creds.region, "tat.tencentcloudapi.com"));
}

export function createCdbClient(creds: TencentCredentials, region?: string) {
  return new CdbClient(clientConfig(creds, region ?? creds.cdbRegion, "cdb.tencentcloudapi.com"));
}

export function createCosClient(creds: TencentCredentials) {
  return new COS({
    SecretId: creds.secretId,
    SecretKey: creds.secretKey
  });
}

