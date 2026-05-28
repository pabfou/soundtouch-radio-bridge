FROM golang:1.22-alpine AS builder
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o bridge .

FROM scratch
# Needed for the bridge to verify TLS certificates when talking to HTTPS streams.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /app/bridge /bridge
EXPOSE 8080
CMD ["/bridge", "--config", "/config.yaml"]
