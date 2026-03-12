FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev libwebp-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates libwebp
WORKDIR /app
COPY --from=builder /app/server .
COPY --from=builder /app/migrations ./migrations
COPY --from=builder /app/web ./web

RUN mkdir -p uploads/preview uploads/thumb uploads/full

EXPOSE 8080
CMD ["./server"]
