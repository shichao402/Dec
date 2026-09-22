import type { CdbConnectionConfig } from "../lib.js";
export type MysqlPool = {
    query<T = unknown>(sql: string, values?: unknown[]): Promise<[T, unknown]>;
    end(): Promise<void>;
};
export declare function createMysqlPool(config: CdbConnectionConfig): Promise<MysqlPool>;
export declare function resolveCdbConnectionFromEnv(env: Record<string, string>, database?: string): CdbConnectionConfig | null;
