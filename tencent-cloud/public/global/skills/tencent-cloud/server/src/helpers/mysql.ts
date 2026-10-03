import type { CdbConnectionConfig } from "../lib.js";

export type MysqlPool = {
  query<T = unknown>(sql: string, values?: unknown[]): Promise<[T, unknown]>;
  end(): Promise<void>;
};

export async function createMysqlPool(config: CdbConnectionConfig): Promise<MysqlPool> {
  const mysql = await import("mysql2/promise");
  return mysql.createPool({
    host: config.host,
    port: config.port,
    user: config.user,
    password: config.password,
    database: config.database || undefined,
    waitForConnections: true,
    connectionLimit: 2,
    connectTimeout: 10000
  });
}

export function resolveCdbConnectionFromEnv(
  env: Record<string, string>,
  database?: string
): CdbConnectionConfig | null {
  const host = env.CDB_HOST ?? env.MYSQL_HOST ?? "";
  const user = env.CDB_USER ?? env.MYSQL_USER ?? "";
  const password = env.CDB_PASSWORD ?? env.MYSQL_PASSWORD ?? "";
  if (!host || !user || !password) {
    return null;
  }
  return {
    host,
    port: Number(env.CDB_PORT ?? env.MYSQL_PORT ?? "3306"),
    user,
    password,
    database: database ?? env.CDB_DATABASE ?? env.MYSQL_DATABASE ?? ""
  };
}
