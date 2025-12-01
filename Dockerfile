FROM golang:1.25-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/crawler ./cmd/crawler

FROM debian:bookworm-slim
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates chromium && \
    rm -rf /var/lib/apt/lists/*
COPY --from=builder /bin/crawler /usr/local/bin/crawler
ENV BYTEFETCH_BROWSER_EXECUTABLE_PATH=/usr/bin/chromium
ENTRYPOINT ["/usr/local/bin/crawler"]
