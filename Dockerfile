# syntax=docker/dockerfile:1.6
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/pqmedia-api ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/pqmedia-migrate ./cmd/migrate

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/pqmedia-api /usr/local/bin/pqmedia-api
COPY --from=build /out/pqmedia-migrate /usr/local/bin/pqmedia-migrate
COPY db/migrations /app/db/migrations
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/pqmedia-api"]
