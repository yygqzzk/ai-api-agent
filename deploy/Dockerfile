FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/api-assistant ./cmd/server

FROM alpine:3.21
WORKDIR /app
COPY --from=builder /out/api-assistant /usr/local/bin/api-assistant
COPY config ./config
COPY skills ./skills
COPY testdata ./testdata
EXPOSE 8080
CMD ["api-assistant", "run"]
