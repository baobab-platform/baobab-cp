# CP Console image (ADR-BCP-019 sections 89-90). Build from the repository
# root, which holds the pnpm workspace and its lockfile:
#   docker build --file frontend/Dockerfile .
FROM node:24.21.0-trixie-slim@sha256:8ec5d7557396cfe32d21c3f9c13072355ceab22b584578ca4bb28af31120cffe AS build
WORKDIR /workspace
ENV NEXT_TELEMETRY_DISABLED=1
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY frontend/package.json frontend/
RUN pnpm install --frozen-lockfile
COPY frontend frontend
RUN pnpm --filter @baobab-platform/cp-console build

FROM gcr.io/distroless/nodejs24-debian13:nonroot@sha256:bb6b03d81066993293a10feda7250e8e1cc034035fe9b61cfceededa7c8bf04d
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
