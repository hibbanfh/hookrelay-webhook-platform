# syntax=docker/dockerfile:1.7
# One image, several processes (ADR 0002). No ENTRYPOINT on purpose: compose and Fly
# pass the full command, e.g. "/hookrelay worker". Build context: ./backend

FROM golang:1.26-bookworm AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY go.mod go.sum ./
# Optional extra CA certificates (for example a TLS-inspecting antivirus or corporate proxy), appended
# to the system bundle. Supplied only by a local docker-compose.override.yml; absent in CI and production.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=secret,id=extra_ca,required=false \
    if [ -s /run/secrets/extra_ca ]; then \
      cat /etc/ssl/certs/ca-certificates.crt /run/secrets/extra_ca > /tmp/ca-bundle.crt; \
      export SSL_CERT_FILE=/tmp/ca-bundle.crt; \
    fi; \
    go mod download
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
