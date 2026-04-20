# Build
FROM --platform=$BUILDPLATFORM golang:1.24.6-bookworm@sha256:ab1d1823abb55a9504d2e3e003b75b36dbeb1cbcc4c92593d85a84ee46becc6c AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags="-s -w" -o /app/openserp .

# `chromedp/headless-shell:stable` also works here
FROM chromedp/headless-shell:stable@sha256:aac539266027f91cf47610da1129dce360d23f45f8f150683cca94223fa2f1e2

WORKDIR /usr/src/app

# wget: used by HEALTHCHECK (localhost, no TLS, so ca-certificates not required).
# dumb-init: already provided by `docker run --init` / compose `init: true`, so we do NOT add tini here — the PID1 reaper is supplied by the runtime.
RUN apt-get update \
  && apt-get install -y --no-install-recommends wget \
  && rm -rf /var/lib/apt/lists/* \
  && getent passwd chrome >/dev/null 2>&1 || useradd --create-home --uid 1001 --shell /bin/bash chrome \
  && chown chrome:chrome /usr/src/app

COPY --from=builder /app/openserp /usr/local/bin/openserp
COPY --chown=chrome:chrome config.yaml ./config.yaml

# Rod's launcher.LookPath does not know about /headless-shell/headless-shell.
# Viper auto-binds OPENSERP_APP_BROWSER_PATH to app.browser_path, so this pins the binary and avoids Rod's runtime chromium auto-download (which would fail in this non-root, network-restricted image).
ENV OPENSERP_APP_BROWSER_PATH=/headless-shell/headless-shell \
  OPENSERP_SERVER_HOST=0.0.0.0 \
  OPENSERP_SERVER_PORT=7000

USER chrome

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget --quiet --tries=1 --spider "http://127.0.0.1:${OPENSERP_SERVER_PORT}/health" || exit 1

ENTRYPOINT ["openserp"]
