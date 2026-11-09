# syntax=docker/dockerfile:1.7
# One image, several processes (ADR 0002). No ENTRYPOINT on purpose: compose and Fly
# pass the full command, e.g. "/hookrelay worker". Build context: ./backend

FROM golang:1.26-bookworm AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w -X main.version=${VERSION}" -o /out/hookrelay ./cmd/server

# distroless static: no shell, no package manager, CA certs + tzdata included, non-root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hookrelay /hookrelay
USER 65532:65532
EXPOSE 8080

CMD ["/hookrelay", "api"]
