# Build stage
FROM golang:1.25.3 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -v -o bin/portage-server cmd/server/main.go && \
    go build -v -o bin/portage-dashboard cmd/dashboard/main.go && \
    go build -v -o bin/portage-builder cmd/builder/main.go

# Runtime stage - Gentoo base with emerge
FROM gentoo/stage3:latest

WORKDIR /app

# Install Go runtime and necessary tools
RUN emerge -q app-eselect/eselect dev-lang/go dev-vcs/git && \
    go version

# Copy built binaries from builder
COPY --from=builder /app/bin /app/bin
COPY --from=builder /app/cmd /app/cmd
COPY --from=builder /app/internal /app/internal
COPY --from=builder /app/pkg /app/pkg
COPY --from=builder /app/configs /app/configs
COPY --from=builder /app/go.mod /app/go.sum /app/

# Copy Go cache from builder to speed up future builds
COPY --from=builder /go/pkg /go/pkg

RUN chmod +x /app/bin/*

ENV PATH="/app/bin:${PATH}"
ENV GOPATH=/go

EXPOSE 8080 8081 9090

CMD ["/app/bin/portage-server"]
