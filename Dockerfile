FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /emailtracker .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /emailtracker /app/emailtracker
COPY templates ./templates
COPY static ./static

ENV PORT=8080
EXPOSE 8080

CMD ["/app/emailtracker"]
