FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o emby302 ./cmd/server/

FROM alpine:3.19

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=builder /app/emby302 .

RUN mkdir -p /data

EXPOSE 8095 8098

ENTRYPOINT ["./emby302"]
CMD ["--data-dir=/data"]
