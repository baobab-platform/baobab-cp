# CP Console image (ADR-BCP-019 sections 89-90). Build from the repository
# root, which holds the pnpm workspace and its lockfile:
#   docker build --file frontend/Dockerfile .
FROM node:26.13.0-trixie-slim@sha256:e4db4c914bf8ad241bd24804fc25d272d89bad97e68cdc8a1414862790a02c39 AS build
WORKDIR /workspace
ENV NEXT_TELEMETRY_DISABLED=1
# Node 26 no longer bundles Corepack: install the reviewed exact version.
RUN npm install --global --no-audit --no-fund corepack@0.36.0 && \
    corepack enable && test "$(corepack --version)" = "0.36.0"
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml .npmrc ./
RUN test "$(node --version)" = "v26.13.0" && \
    test "$(pnpm --version)" = "12.16.1"
COPY frontend/package.json frontend/
RUN --mount=type=secret,id=npm_token,required=true \
    --mount=type=tmpfs,target=/tmp/bxs-npm-auth \
    sh -eu -c 'printf "//npm.pkg.github.com/:_authToken=%s\n" "$(cat /run/secrets/npm_token)" > /tmp/bxs-npm-auth/.npmrc; \
      NPM_CONFIG_USERCONFIG=/tmp/bxs-npm-auth/.npmrc pnpm install --frozen-lockfile'
COPY frontend frontend
RUN pnpm --filter @baobab-platform/cp-console build

# Exact published Node 26 distroless candidate; revalidate pull/signature in CI before merge.
FROM gcr.io/distroless/nodejs26-debian13:nonroot@sha256:9b945e92c1b5463a4b75f11e8417515e4bea7355f6adfd79ec220fbbe905dda8
ARG VERSION=0.0.0-dev
ARG REVISION=unknown
LABEL org.opencontainers.image.source="https://github.com/baobab-platform/baobab-cp" \
      org.opencontainers.image.title="baobab-cp-console" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=3000 HOSTNAME=0.0.0.0
# The standalone output mirrors the workspace layout: the server is at
# frontend/server.js.
COPY --from=build --chown=nonroot:nonroot /workspace/frontend/.next/standalone ./
COPY --from=build --chown=nonroot:nonroot /workspace/frontend/.next/static ./frontend/.next/static
COPY --from=build --chown=nonroot:nonroot /workspace/frontend/healthcheck.mjs ./
USER nonroot:nonroot
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD ["/nodejs/bin/node", "healthcheck.mjs"]
CMD ["frontend/server.js"]
