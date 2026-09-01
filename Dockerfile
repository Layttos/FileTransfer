# ---- 1. Compilation du binaire Go ----
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Les dependances d'abord : cette couche est mise en cache tant que go.mod/go.sum ne bougent pas.
COPY go.mod go.sum ./
RUN go mod download

# Puis le code source, et compilation statique.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o server .

# ---- 2. Image d'execution minimale ----
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata wget

WORKDIR /app

COPY --from=builder /app/server .

# Les pages HTML sont servies depuis le disque a l'execution : elles doivent etre dans l'image.
COPY public ./public

# Point de montage du stockage des fichiers (volume declare dans docker-compose.yml).
RUN mkdir -p /app/uploads

EXPOSE 3333

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:3333/ >/dev/null 2>&1 || exit 1

CMD ["./server"]
