# syntax=docker/dockerfile:1.7

FROM node:20-alpine AS build

WORKDIR /src
COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci

COPY . .
ARG VITE_API_BASE_URL=/api/v1
ARG VITE_ASSET_BASE_URL=
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}
ENV VITE_ASSET_BASE_URL=${VITE_ASSET_BASE_URL}
RUN npm run build

FROM nginx:1.28-alpine AS runtime

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist/ /usr/share/nginx/html/

EXPOSE 80
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=4 \
    CMD wget -q -O /dev/null http://127.0.0.1/healthz || exit 1

