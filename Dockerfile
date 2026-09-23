FROM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api ./api
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/controlplane ./cmd/controlplane
FROM gcr.io/distroless/static-debian13:nonroot
ARG VERSION=0.0.0-dev
ARG REVISION=unknown
LABEL org.opencontainers.image.source="https://github.com/baobab-platform/baobab-cp" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
COPY --from=build /out/controlplane /controlplane
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD ["/controlplane", "healthcheck"]
ENTRYPOINT ["/controlplane"]
