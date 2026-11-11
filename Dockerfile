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
FROM chromedp/headless-shell:stable@sha256:f7e7ac721b023cb8717f8108aef8b3e49995fb1e5a912f41e570c29e45d24961

WORKDIR /usr/src/app

# wget: used by HEALTHCHECK (localhost, no TLS, so ca-certificates not required).
# tini: reaps orphaned Chrome children, which would otherwise pile up as zombies when openserp is PID 1.
RUN apt-get update \
  && apt-get install -y --no-install-recommends wget tini \
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

# -s keeps reaping when another init is PID 1 (`docker run --init`, compose `init: true`).
ENTRYPOINT ["/usr/bin/tini", "-s", "--", "openserp"]
