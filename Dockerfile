FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o bridge .

FROM scratch
COPY --from=builder /app/bridge /bridge
EXPOSE 8080
CMD ["/bridge", "--config", "/config.yaml"]
