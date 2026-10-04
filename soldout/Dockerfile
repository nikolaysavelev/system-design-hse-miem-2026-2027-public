# syntax=docker/dockerfile:1.7
# Multi-stage: сборка в golang:1.27-alpine, запуск в distroless (без shell; healthcheck — флаг -healthcheck).
FROM golang:1.27-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w -X main.version=${VERSION}" -o /out/soldout ./cmd/soldout && \
    go build -ldflags="-s -w" -o /out/invariant-checker ./tools/invariant-checker && \
    go build -ldflags="-s -w" -o /out/mailgw ./cmd/mailgw && \
    go build -ldflags="-s -w -X main.version=${VERSION}" -o /out/notifier ./cmd/notifier

# Если gcr.io недоступен, замените базовый образ на alpine:3.22 (тогда healthcheck можно делать через wget).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/soldout /soldout
COPY --from=build /out/invariant-checker /invariant-checker
COPY --from=build /out/mailgw /mailgw
COPY --from=build /out/notifier /notifier
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/soldout"]
