# BookLoop Frontend

Vue 3 + TypeScript + Vite frontend for BookLoop.

## Local development

```bash
cp .env.example .env
npm install
npm run dev
```

Vite proxies `/api` and `/uploads` to `VITE_DEV_PROXY_TARGET` (default `http://127.0.0.1:8080`).

## Checks

```bash
npm run typecheck
npm test
npm run build
```

Authentication is stored in `sessionStorage`, so it survives a reload in the current tab but is cleared when the tab session ends. The global Axios interceptor adds the bearer token and `X-Request-ID`; it deliberately does not add `Idempotency-Key`. Purchase intents create and persist that key explicitly until an order succeeds or the user deliberately starts a new intent.

