FROM node:22-alpine AS assets

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY tailwind.config.js ./
COPY static/css/input.css ./static/css/input.css
COPY templates ./templates

RUN npm run build:css

FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=assets /app/static/css/tailwind.css ./static/css/tailwind.css

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /emailtracker .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /emailtracker /app/emailtracker
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/static ./static

ENV PORT=8080
EXPOSE 8080

CMD ["/app/emailtracker"]
