# Built from the repo root: docker build -f deploy/env/web.Dockerfile .
FROM node:22-alpine AS build
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM caddy:2-alpine
COPY deploy/env/Caddyfile /etc/caddy/Caddyfile
COPY deploy/env/validate-caddy.sh /tmp/validate-caddy.sh
RUN sh /tmp/validate-caddy.sh /etc/caddy/Caddyfile && rm /tmp/validate-caddy.sh
COPY --from=build /app/dist /srv
