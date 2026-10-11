import { randomBytes } from "node:crypto";

import { describe, expect, it } from "vitest";

import { OidcBoundaryError, type PendingAuthorization } from "@/server/auth/oidc-protocol";
import { RedisConfidentialSessionStore, type RedisCommands } from "@/server/auth/redis-sessions";

class FakeRedis implements RedisCommands {
  readonly values = new Map<string, string>();
  readonly expiry = new Map<string, number>();
  constructor(private readonly now: () => number) {}
  async execute(...args: string[]): Promise<string | null> {
    const [command, key, value, condition, ttlSpecifier, ttlValue] = args;
    if (!command || !key) throw new Error("invalid redis operation");
    if ((this.expiry.get(key) ?? Infinity) <= this.now()) {
      this.values.delete(key);
      this.expiry.delete(key);
    }
    if (command === "SET") {
      if (condition !== "NX" || ttlSpecifier !== "PX" || !value ||
        !ttlValue || Number(ttlValue) < 1 || this.values.has(key)) return null;
      this.values.set(key, value);
      this.expiry.set(key, this.now() + Number(ttlValue));
      return "OK";
    }
    const current = this.values.get(key) ?? null;
    if (command === "GETDEL" || command === "DEL") {
      this.values.delete(key);
      this.expiry.delete(key);
    }
    return command === "DEL" ? "1" : current;
  }
}

function setup() {
  let at = 1_800_000_000_000;
  const clock = () => at;
  const redis = new FakeRedis(clock);
  const store = new RedisConfidentialSessionStore(redis, randomBytes(32), clock);
  return { store, redis, advance: (ms: number) => { at += ms; }, now: clock };
}

function pending(now: number): PendingAuthorization {
  return {
    state: randomBytes(32).toString("base64url"),
    nonce: randomBytes(32).toString("base64url"),
    codeVerifier: randomBytes(32).toString("base64url"),
    redirectUri: "https://console.example.test/api/auth/callback",
    issuer: "https://iam.example.test/realms/baobab",
    clientId: "cp-console",
    createdAt: now,
    expiresAt: now + 300_000,
  };
}

describe("FE-03B protected cross-instance session custody", () => {
  it("persists encrypted one-time authorization material and refuses replay", async () => {
    const { store, redis, now } = setup();
    const source = pending(now());
    const handle = await store.issueAuthorization(source);
    const ciphertext = [...redis.values.values()][0] ?? "";
    const storageKey = [...redis.values.keys()][0] ?? "";
    expect(storageKey).not.toContain(handle);
    expect(ciphertext).not.toContain(source.nonce);
    expect(ciphertext).not.toContain(source.codeVerifier);
    expect(await store.consumeAuthorization(handle)).toEqual(source);
    expect(await store.consumeAuthorization(handle)).toBeNull();
  });

  it("denies an expired pending authorization and malformed session handles", async () => {
    const { store, advance, now } = setup();
    const handle = await store.issueAuthorization(pending(now()));
    advance(300_001);
    expect(await store.consumeAuthorization(handle)).toBeNull();
    await expect(store.consumeAuthorization("../../../etc/passwd")).rejects.toThrow(OidcBoundaryError);
  });

  it("keeps a token server-side and revokes the opaque session immediately", async () => {
    const { store, redis, now } = setup();
    const accessToken = "sensitive-access-token-only-the-server-may-read";
    const handle = await store.createSession("principal-a", accessToken, now() + 60_000);
    expect(handle).toMatch(/^[A-Za-z0-9_-]{43}$/);
    expect(JSON.stringify([...redis.values.values()])).not.toContain(accessToken);
    const session = await store.readSession(handle);
    expect(session?.accessToken).toBe(accessToken);
    expect(session?.principalId).toBe("principal-a");
    expect(session?.csrfToken).toMatch(/^[A-Za-z0-9_-]{43}$/);
    await store.revokeSession(handle);
    expect(await store.readSession(handle)).toBeNull();
  });

  it("rejects tampered or swapped ciphertext even if Redis returns a string", async () => {
    const { store, redis, now } = setup();
    const first = await store.createSession("principal-one", "one-access-token", now() + 60_000);
    const second = await store.createSession("principal-two", "two-access-token", now() + 60_000);
    const keys = [...redis.values.keys()];
    const values = [...redis.values.values()];
    redis.values.set(keys[0]!, values[1]!);
    await expect(store.readSession(first)).rejects.toThrow(OidcBoundaryError);
    expect((await store.readSession(second))?.principalId).toBe("principal-two");
  });

  it("refuses an invalid encryption key and excessively long or expired custody", async () => {
    const { redis, now } = setup();
    expect(() => new RedisConfidentialSessionStore(redis, randomBytes(16), now)).toThrow(OidcBoundaryError);
    const store = new RedisConfidentialSessionStore(redis, randomBytes(32), now);
    await expect(store.createSession("principal", "access", now() + 3_600_001)).rejects.toThrow(OidcBoundaryError);
    await expect(store.createSession("principal", "access", now() - 1)).rejects.toThrow(OidcBoundaryError);
  });
});
