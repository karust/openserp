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

COPY --from=builder /app/openserp /usr/local/bin/openserp
ADD config.yaml /usr/src/app

ENTRYPOINT ["openserp"]

