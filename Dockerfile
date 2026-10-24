# --- Этап 1: компиляция ---
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Кэшируем зависимости отдельно от исходников
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/spidometr .

# --- Этап 2: минимальный образ ---
FROM alpine:3.22

# Корневые сертификаты нужны для HTTPS-запросов к CDN
RUN apk add --no-cache ca-certificates \
    && adduser -D -H -u 10001 spidometr

COPY --from=builder /out/spidometr /usr/local/bin/spidometr

USER spidometr
ENTRYPOINT ["spidometr"]
CMD ["-source=yandex", "-threads=4", "-time=7"]
