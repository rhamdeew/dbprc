FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/rhamdeew/dbprc/backend/internal/config.Version=${VERSION}" \
    -o /out/arc ./cmd/arc

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 arc \
    && mkdir -p /data /uploads \
    && chown arc:arc /data /uploads
COPY --from=build /out/arc /usr/local/bin/arc
USER arc
ENV PORT=8080
EXPOSE 8080
VOLUME ["/data", "/uploads"]
HEALTHCHECK --interval=30s --timeout=3s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/arc"]
