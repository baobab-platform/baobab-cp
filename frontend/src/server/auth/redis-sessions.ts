import "server-only";

import { createCipheriv, createDecipheriv, createHash, randomBytes } from "node:crypto";
import { connect as tlsConnect } from "node:tls";
import type { TLSSocket } from "node:tls";

import {
  OidcBoundaryError, type ConfidentialSessionStore, type PendingAuthorization,
} from "./oidc-protocol";

export interface RedisCommands {
  /** RESP values are opaque; never log command arguments or returned secrets. */
  execute(...args: string[]): Promise<string | null>;
}

function redisMessage(args: readonly string[]): Buffer {
  const parts = [Buffer.from(`*${args.length}\r\n`)];
  for (const arg of args) {
    const value = Buffer.from(arg, "utf8");
    parts.push(Buffer.from(`$${value.length}\r\n`), value, Buffer.from("\r\n"));
  }
  return Buffer.concat(parts);
}

function parseRedisReply(data: Buffer): { length: number; value: string | null; failed: boolean } | null {
  if (data.length < 4) return null;
  const prefix = String.fromCharCode(data[0] ?? 0);
  if (!["+", "-", ":", "$"].includes(prefix)) throw new OidcBoundaryError();
  const separator = data.indexOf("\r\n", 1);
  if (separator < 0) return null;
  const header = data.toString("utf8", 1, separator);
  if (header.length > 64 && prefix !== "-") throw new OidcBoundaryError();
  if (prefix !== "$") {
    if (header.length > 512) throw new OidcBoundaryError();
    return { length: separator + 2, value: header, failed: prefix === "-" };
  }
  if (!/^-?\d+$/.test(header)) throw new OidcBoundaryError();
  const size = Number(header);
  if (!Number.isSafeInteger(size) || size < -1 || size > 1_048_576) throw new OidcBoundaryError();
  if (size === -1) return { length: separator + 2, value: null, failed: false };
  if (data.length < separator + 2 + size + 2) return null;
  const end = separator + 2 + size;
  if (data[end] !== 13 || data[end + 1] !== 10) throw new OidcBoundaryError();
  return { length: end + 2, value: data.toString("utf8", separator + 2, end), failed: false };
}

export type RedisTlsConfig = {
  host: string;
  port: number;
  user: string;
  password: string;
  timeoutMs?: number;
};

/**
 * Minimal pinned Redis RESP2 TLS transport, intentionally dependency-free.
 * One authenticated connection per operation prevents mixing caller contexts.
 * Redis must implement GETDEL (6.2+), SET NX PX, GET and DEL.
 */
export class TlsRedisCommands implements RedisCommands {
  constructor(private readonly config: RedisTlsConfig) {
    if (!/^[a-z0-9][a-z0-9.-]{0,251}$/i.test(config.host) || config.host === "localhost" ||
        !Number.isInteger(config.port) || config.port < 1 || config.port > 65535 ||
        !config.user || !config.password || config.password.length > 4096 ||
        (config.timeoutMs !== undefined && (config.timeoutMs < 100 || config.timeoutMs > 10_000))) {
      throw new OidcBoundaryError();
    }
  }

  async execute(...command: string[]): Promise<string | null> {
    if (command.length < 1 || command.some((v) => typeof v !== "string" || v.length > 1_048_576)) {
      throw new OidcBoundaryError();
    }
    const { host, port, user, password } = this.config;
    return await new Promise<string | null>((resolve, reject) => {
      let settled = false;
      let buffer = Buffer.alloc(0);
      let received = 0;
      let replies = 0;
      let socket: TLSSocket;
      const finish = (err?: unknown, result?: string | null) => {
        if (settled) return;
        settled = true;
        socket.destroy();
        if (err) reject(new OidcBoundaryError());
        else resolve(result ?? null);
      };
      try {
        socket = tlsConnect({
          host, port, servername: host, rejectUnauthorized: true, minVersion: "TLSv1.2",
        });
      } catch {
        reject(new OidcBoundaryError());
        return;
      }
      socket.setTimeout(this.config.timeoutMs ?? 3000, () => finish(true));
      socket.on("error", () => finish(true));
      socket.on("end", () => finish(true));
      socket.on("secureConnect", () => {
        socket.write(Buffer.concat([redisMessage(["AUTH", user, password]), redisMessage(command)]));
      });
      socket.on("data", (chunk: Buffer) => {
        if (settled) return;
        received += chunk.length;
        if (received > 2_100_000) { finish(true); return; }
        buffer = Buffer.concat([buffer, chunk]);
        try {
          while (true) {
            const message = parseRedisReply(buffer);
            if (!message) break;
            buffer = buffer.subarray(message.length);
            if (message.failed) { finish(true); return; }
            // First reply authenticates this connection; second is its single command.
            replies++;
            if (replies === 1 && message.value !== "OK") { finish(true); return; }
            if (replies === 2) { finish(undefined, message.value); return; }
          }
        } catch {
          finish(true);
        }
      });
    });
  }
  }

/**
 * Server-only, tenant-agnostic opaque session store. Redis keys contain hashes
 * of random handles, not handles or JWTs. Every payload uses AES-256-GCM with
 * a fresh nonce and the Redis key as authenticated additional data.
 */
export class RedisConfidentialSessionStore implements ConfidentialSessionStore {
  constructor(
    private readonly redis: RedisCommands,
    private readonly key: Buffer,
    private readonly now: () => number = Date.now,
  ) {
    if (key.length !== 32) throw new OidcBoundaryError();
  }

  private storageKey(kind: "auth" | "session", handle: string): string {
    if (!/^[A-Za-z0-9_-]{43}$/.test(handle)) throw new OidcBoundaryError();
    return `baobab:cp-console:${kind}:${createHash("sha256").update(handle).digest("hex")}`;
  }

  private seal(storageKey: string, value: unknown): string {
    const nonce = randomBytes(12);
    const cipher = createCipheriv("aes-256-gcm", this.key, nonce);
    cipher.setAAD(Buffer.from(storageKey, "utf8"));
    const plaintext = Buffer.from(JSON.stringify(value), "utf8");
    const encrypted = Buffer.concat([cipher.update(plaintext), cipher.final()]);
    return Buffer.concat([nonce, cipher.getAuthTag(), encrypted]).toString("base64url");
  }

  private open(storageKey: string, secret: string): unknown {
    try {
      const buffer = Buffer.from(secret, "base64url");
      if (buffer.length < 29 || buffer.length > 65_536) throw new OidcBoundaryError();
      const decipher = createDecipheriv("aes-256-gcm", this.key, buffer.subarray(0, 12));
      decipher.setAAD(Buffer.from(storageKey, "utf8"));
      decipher.setAuthTag(buffer.subarray(12, 28));
      return JSON.parse(Buffer.concat([decipher.update(buffer.subarray(28)), decipher.final()]).toString("utf8"));
    } catch {
      throw new OidcBoundaryError();
    }
  }

  async issueAuthorization(pending: PendingAuthorization): Promise<string> {
    const ttl = pending.expiresAt - this.now();
    if (!Number.isSafeInteger(ttl) || ttl < 1 || ttl > 300_000 ||
        !/^[A-Za-z0-9_-]{43}$/.test(pending.state)) {
      throw new OidcBoundaryError();
    }
    const handle = randomBytes(32).toString("base64url");
    const storageKey = this.storageKey("auth", handle);
    const result = await this.redis.execute("SET", storageKey, this.seal(storageKey, pending),
      "NX", "PX", String(ttl));
    if (result !== "OK") throw new OidcBoundaryError();
    return handle;
  }

  async consumeAuthorization(handle: string): Promise<PendingAuthorization | null> {
    const key = this.storageKey("auth", handle);
    const encrypted = await this.redis.execute("GETDEL", key);
    if (encrypted === null) return null;
    const value = this.open(key, encrypted) as PendingAuthorization;
    if (!value || !Number.isSafeInteger(value.expiresAt) || value.expiresAt <= this.now() ||
        !Number.isSafeInteger(value.createdAt) || value.expiresAt - value.createdAt > 300_000 ||
        !/^[A-Za-z0-9_-]{43}$/.test(value.state) ||
        !/^[A-Za-z0-9_-]{43}$/.test(value.nonce) ||
        !/^[A-Za-z0-9_-]{43}$/.test(value.codeVerifier) ||
        typeof value.issuer !== "string" || typeof value.clientId !== "string" ||
        typeof value.redirectUri !== "string") {
      throw new OidcBoundaryError();
    }
    return value;
  }

  async createSession(principalId: string, opaqueTokenHandle: string, expiresAt: number): Promise<string> {
    const ttl = expiresAt - this.now();
    if (!principalId || principalId.length > 256 || !opaqueTokenHandle ||
        opaqueTokenHandle.length > 16384 || !Number.isSafeInteger(ttl) ||
        ttl < 1 || ttl > 3_600_000) {
      throw new OidcBoundaryError();
    }
    const handle = randomBytes(32).toString("base64url");
    const key = this.storageKey("session", handle);
    const result = await this.redis.execute("SET", key, this.seal(key, {
      principalId, accessToken: opaqueTokenHandle, expiresAt,
      csrfToken: randomBytes(32).toString("base64url"),
    }), "NX", "PX", String(ttl));
    if (result !== "OK") throw new OidcBoundaryError();
    return handle;
  }

  async readSession(handle: string): Promise<{ principalId: string; accessToken: string; csrfToken: string; expiresAt: number } | null> {
    const key = this.storageKey("session", handle);
    const encrypted = await this.redis.execute("GET", key);
    if (encrypted === null) return null;
    const value = this.open(key, encrypted) as {
      principalId: string; accessToken: string; csrfToken: string; expiresAt: number;
    };
    if (!value || typeof value.principalId !== "string" || !value.principalId ||
        typeof value.accessToken !== "string" || !value.accessToken ||
        typeof value.csrfToken !== "string" || !/^[A-Za-z0-9_-]{43}$/.test(value.csrfToken) ||
        !Number.isSafeInteger(value.expiresAt) || value.expiresAt <= this.now()) {
      throw new OidcBoundaryError();
    }
    return value;
  }

  async revokeSession(handle: string): Promise<void> {
    const key = this.storageKey("session", handle);
    await this.redis.execute("DEL", key);
  }
}
