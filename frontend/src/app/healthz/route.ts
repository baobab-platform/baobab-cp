// Liveness for the container healthcheck. It reports only that this
// process serves requests: no configuration, version or dependency detail.
export const dynamic = "force-dynamic";

export function GET(): Response {
  return new Response("ok", { status: 200, headers: { "Cache-Control": "no-store", "Content-Type": "text/plain" } });
}
