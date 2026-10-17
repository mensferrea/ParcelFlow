FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/parcelflow ./main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/parcelflow .
COPY --from=builder /app/migrations ./migrations

EXPOSE 8080 50051 8082
CMD ["./parcelflow"]
