export type WorkspaceCommand = {
  action: string;
  id?: string;
  body?: Record<string, unknown>;
  idempotencyKey?: string;
};

export type ApplicationView = {
  client_application_id: string;
  reference: string;
  status: string;
  version: number;
  business_identity?: Record<string, unknown>;
  requested_markets?: { country_code: string }[];
};

type SessionResponse = { authenticated: boolean; csrfToken: string };

export async function sessionCsrf(): Promise<string> {
  const response = await fetch("/api/auth/session", { method: "GET", credentials: "same-origin", cache: "no-store" });
  if (!response.ok) throw new Error("Your session is no longer available. Please sign in again.");
  const session = await response.json() as SessionResponse;
  if (!session.authenticated || !/^[A-Za-z0-9_-]{43}$/.test(session.csrfToken)) {
    throw new Error("Your session is no longer available. Please sign in again.");
  }
  return session.csrfToken;
}

/** Browser never sees CP/IAM bearer, only CSRF and its own application payloads. */
export async function sendWorkspaceCommand<T = unknown>(command: WorkspaceCommand): Promise<T> {
  const csrf = await sessionCsrf();
  const response = await fetch("/api/workspace/v2", {
    method: "POST",
    credentials: "same-origin",
    cache: "no-store",
    headers: { "Content-Type": "application/json", "x-csrf-token": csrf },
    body: JSON.stringify({ ...command, idempotencyKey: command.idempotencyKey ?? crypto.randomUUID() }),
  });
  if (!response.ok) {
    throw new Error(`Control Plane denied or could not complete this operation (HTTP ${response.status}).`);
  }
  return await response.json() as T;
}

export async function readWorkspace<T = unknown>(action: string, id?: string, tenantId?: string): Promise<T> {
  const query = new URLSearchParams({ action });
  if (id) query.set("id", id);
  if (tenantId) query.set("tenantId", tenantId);
  const response = await fetch(`/api/workspace/v2?${query}`, {
    method: "GET", credentials: "same-origin", cache: "no-store",
  });
  if (!response.ok) {
    throw new Error(`The current governed record is unavailable or access is denied (HTTP ${response.status}).`);
  }
  return await response.json() as T;
}
