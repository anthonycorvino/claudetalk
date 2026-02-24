FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o claudetalk-server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/claudetalk-server /usr/local/bin/claudetalk-server

EXPOSE 8080
CMD ["claudetalk-server", "--port", "8080", "--file-dir", "/data/files"]
