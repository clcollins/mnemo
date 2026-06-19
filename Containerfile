FROM golang:1.26 AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /mnemo ./cmd/mnemo

FROM registry.fedoraproject.org/fedora-minimal:43

LABEL org.opencontainers.image.title="mnemo" \
      org.opencontainers.image.description="Local-first agent memory and retrieval MCP server" \
      org.opencontainers.image.source="https://github.com/clcollins/mnemo"

COPY --from=builder /mnemo /usr/local/bin/mnemo

USER 65534
ENTRYPOINT ["/usr/local/bin/mnemo"]
CMD ["serve"]
