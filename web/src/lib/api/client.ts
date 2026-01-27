/**
 * NTM API Client
 *
 * Type-safe REST client generated from OpenAPI spec using openapi-fetch.
 * Supports authentication and automatic error handling.
 */

import createClient from "openapi-fetch";
import type { paths } from "./schema";

// Connection configuration stored in localStorage
export interface ConnectionConfig {
  baseUrl: string;
  authToken?: string;
}

const CONNECTION_KEY = "ntm-connection";

/**
 * Get the current connection config from localStorage.
 */
export function getConnectionConfig(): ConnectionConfig | null {
  if (typeof window === "undefined") return null;
  const stored = localStorage.getItem(CONNECTION_KEY);
  if (!stored) return null;
  try {
    return JSON.parse(stored) as ConnectionConfig;
  } catch {
    return null;
  }
}

/**
 * Save connection config to localStorage.
 */
export function saveConnectionConfig(config: ConnectionConfig): void {
  if (typeof window === "undefined") return;
  localStorage.setItem(CONNECTION_KEY, JSON.stringify(config));
}

/**
 * Clear connection config from localStorage.
 */
export function clearConnectionConfig(): void {
  if (typeof window === "undefined") return;
  localStorage.removeItem(CONNECTION_KEY);
}

/**
 * Create the NTM API client with current connection config.
 */
export function createNtmClient() {
  const config = getConnectionConfig();
  const baseUrl = config?.baseUrl || process.env.NEXT_PUBLIC_NTM_URL || "http://localhost:8080";

  const client = createClient<paths>({
    baseUrl,
    headers: config?.authToken
      ? { Authorization: `Bearer ${config.authToken}` }
      : undefined,
  });

  return client;
}

/**
 * Singleton client instance.
 * Re-created when connection config changes.
 */
let clientInstance: ReturnType<typeof createNtmClient> | null = null;

/**
 * Get the singleton API client.
 * Call resetClient() when connection config changes.
 */
export function getClient() {
  if (!clientInstance) {
    clientInstance = createNtmClient();
  }
  return clientInstance;
}

/**
 * Reset the client instance (call when connection config changes).
 */
export function resetClient(): void {
  clientInstance = null;
}

/**
 * API error with typed response.
 */
export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public details?: unknown
  ) {
    super(message);
    this.name = "ApiError";
  }
}

/**
 * Check if the server is reachable and authentication is valid.
 */
export async function checkConnection(): Promise<{ ok: boolean; error?: string }> {
  try {
    const client = getClient();
    const response = await client.GET("/api/v1/health");

    if (response.error) {
      return { ok: false, error: `Server error: ${response.error}` };
    }

    return { ok: true };
  } catch (err) {
    const message = err instanceof Error ? err.message : "Connection failed";
    return { ok: false, error: message };
  }
}
