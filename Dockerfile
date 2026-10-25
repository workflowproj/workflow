# One image, three modes (web, hooks, worker): the Go binary with the React UI embedded.
#
#   docker build --build-arg VERSION=$(git describe --tags --always) -t ghcr.io/workflowproj/workflow:dev .
#
# Stages
#   web       Node 22: npm ci + npm run build -> /src/web/dist
#   ui-none   an empty UI (build with --build-arg UI_STAGE=ui-none when web/ is not needed;
#             the binary then serves a "UI not built" page)
#   build     Go: copies the UI into internal/webui/dist and builds a static binary
#   runtime   distroless static, non-root (default target)
#
# The Node and Go stages run on the build platform and cross-compile, so multi-arch builds
# (linux/amd64 + linux/arm64) do not need QEMU for the expensive steps.
# Base images are pinned by digest; Renovate/Dependabot can bump tag and digest together.

ARG NODE_IMAGE=node:22.23.4-alpine3.24@sha256:fe4f6d74e20370acb04ad889077d14d0f64a9c8ea6a42bc60cddf1e844699914
ARG GO_IMAGE=golang:1.26.10-alpine3.24@sha256:27f2d3b83cfc5803c8078f8e730af0f7c431bf067afaec8f60a36fb763187cba
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
# Which stage provides the UI assets: "web" (default) or "ui-none".
ARG UI_STAGE=web

# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS web
ENV CI=true \
    NPM_CONFIG_UPDATE_NOTIFIER=false \
    NPM_CONFIG_FUND=false \
    NPM_CONFIG_AUDIT=false
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
# The OpenAPI spec sits next to web/ so scripts that reference ../api keep working.
COPY api/ /src/api/
COPY web/ ./
RUN npm run build

# ---------------------------------------------------------------------------
FROM scratch AS ui-none
COPY internal/webui/dist/.gitkeep /src/web/dist/.gitkeep

FROM ${UI_STAGE} AS ui

# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/
# Same as `make embed-web`: replace the embed directory with the built UI.
RUN rm -rf internal/webui/dist && mkdir -p internal/webui/dist
COPY --from=ui /src/web/dist/ internal/webui/dist/
ARG TARGETOS TARGETARCH
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    touch internal/webui/dist/.gitkeep && \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -buildvcs=false \
      -ldflags "-s -w -X github.com/workflowproj/workflow/internal/telemetry.Version=${VERSION}" \
      -o /out/wfm ./cmd/wfm

# ---------------------------------------------------------------------------
FROM ${RUNTIME_IMAGE} AS runtime
ARG VERSION=dev
LABEL org.opencontainers.image.title="workflow" \
      org.opencontainers.image.description="Workflow platform: web, hooks and worker modes in one binary" \
      org.opencontainers.image.source="https://github.com/workflowproj/workflow" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY --from=build /out/wfm /wfm
# The Apache License requires the licence and NOTICE with every copy.
COPY LICENSE NOTICE /licenses/
# distroless "nonroot" (65532). Numeric so runAsNonRoot can verify it.
USER 65532:65532
# 8080: UI/API (web) or webhooks/SCIM (hooks). 9090: /metrics, /healthz, /readyz (every mode).
EXPOSE 8080 9090
ENTRYPOINT ["/wfm"]
CMD ["serve"]
