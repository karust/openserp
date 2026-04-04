# Build
FROM golang:alpine as builder

LABEL stage=gobuilder
RUN apk update --no-cache && apk add --no-cache tzdata

WORKDIR /build

ADD go.mod .
ADD go.sum .
RUN go mod download

COPY . .
RUN go build -o /app/openserp .


FROM zenika/alpine-chrome:with-chromedriver

WORKDIR /usr/src/app

COPY --from=builder /app/openserp /usr/local/bin/openserp
COPY config.yaml ./config.yaml

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:7000/health || exit 1

ENTRYPOINT ["openserp"]

