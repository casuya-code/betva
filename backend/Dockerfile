# Betva API image.
#
# Build context is the backend/ directory. The same image ships two binaries:
#   /app/server   the HTTP API
#   /app/migrate  forward-only migration runner (Railway pre-deploy command)

FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
 && mkdir -p /data/media

WORKDIR /app

COPY --from=build /out/server /app/server
COPY --from=build /out/migrate /app/migrate

ENV PORT=8080 \
    ENV=production \
    MEDIA_DIR=/data/media

EXPOSE 8080

CMD ["/app/server"]
