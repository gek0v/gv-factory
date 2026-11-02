# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 gv-factory contributors

# Stage 1: Build the SPA frontend with Node and pnpm
FROM node:22-alpine AS builder

WORKDIR /app

# Enable Corepack to manage pnpm deterministically
RUN corepack enable && corepack prepare pnpm@9.15.4 --activate

# Leverage caching for package dependencies
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

# Copy the complete web application source tree
COPY web/ ./

# Build production distribution assets
RUN pnpm build

# Stage 2: Hardened unprivileged Nginx serving static assets and proxying API/SSE
FROM nginx:alpine-slim

# Install wget for container health probes and configure unprivileged user
RUN apk add --no-cache wget \
    && addgroup -g 10001 -S nonroot \
    && adduser -u 10001 -S -G nonroot -h /home/nonroot nonroot \
    && mkdir -p /var/cache/nginx /var/log/nginx /usr/share/nginx/html /tmp/client_temp /tmp/proxy_temp /tmp/fastcgi_temp /tmp/uwsgi_temp /tmp/scgi_temp \
    && chown -R 10001:10001 /var/cache/nginx /var/log/nginx /usr/share/nginx/html /etc/nginx /tmp

# Deploy built frontend assets
COPY --from=builder /app/dist /usr/share/nginx/html

# Deploy custom unprivileged Nginx configuration with SPA fallback, API & SSE proxying
COPY web/nginx.conf /etc/nginx/nginx.conf

# Switch to unprivileged UID 10001
USER 10001:10001

# Expose web dashboard port
EXPOSE 8080

# Health check configuration probing the /healthz endpoint
HEALTHCHECK --interval=5s --timeout=5s --retries=5 --start-period=5s \
  CMD wget -q --spider http://localhost:8080/healthz || exit 1

CMD ["nginx", "-g", "daemon off;"]
