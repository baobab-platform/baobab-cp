// Operations the pinned Shared control-plane/v1 OpenAPI declares but the
// Control Plane does not serve (FE-00 gap G2). The typed client removes them,
// so Console code cannot call a route that does not exist.
// api.TestOpenAPIDescribesTheRouter holds this list equal to the Control
// Plane's own; update both together.
export const UNIMPLEMENTED_OPERATIONS = [
  "GET /markets/{market_id}",
  "PATCH /markets/{market_id}",
  "POST /markets",
  "POST /markets/{market_id}/activate",
] as const;

export type UnimplementedOperation = (typeof UNIMPLEMENTED_OPERATIONS)[number];
