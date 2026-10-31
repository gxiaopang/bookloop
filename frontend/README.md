# BookLoop Frontend

Vue 3 + TypeScript + Vite frontend for BookLoop.

The interface welcomes all readers, with no campus-specific branding or student
identity requirement. The homepage message is “让好书，遇见更多读者。”

The marketplace sort selector includes “收藏最多”. Select it and click “搜索”
to request `GET /api/v1/books?sort=favorites_desc`. Favorite counts are aggregated
in MySQL in real time, with book ID descending as the tie-breaker. Books with no
favorites remain in the results; keyword, condition and pagination filters still
apply. This requires no Redis cache or database migration.

## Local development

```bash
cp .env.example .env
npm install
npm run dev
```

Vite proxies `/api` and `/uploads` to `VITE_DEV_PROXY_TARGET` (default `http://127.0.0.1:8080`).

The local Docker environments on ports `8088`, `18088`, and `38088` have
independent databases. Existing local accounts are in the `38088` environment.
For this setup, `frontend/.env.local` sets
`VITE_DEV_PROXY_TARGET=http://127.0.0.1:38088`; the frontend remains on `5173`.
Restart Vite after changing proxy environment variables. A value exported in the
shell overrides `.env.local`, so remove an old `VITE_DEV_PROXY_TARGET` override
or update it to the same target. Switching targets does not move accounts.

## Checks

```bash
npm run typecheck
npm test
npm run build
```

Authentication is stored in `sessionStorage`, so it survives a reload in the current tab but is cleared when the tab session ends. The global Axios interceptor adds the bearer token and `X-Request-ID`; it deliberately does not add `Idempotency-Key`. Purchase intents create and persist that key explicitly until an order succeeds or the user deliberately starts a new intent.

After email-verified registration, the frontend automatically signs in using the
existing login API and returns to the original page. If sign-in fails after the
account is created, the form switches to login and explains that registration
has already succeeded. Contacting a seller opens their QR image in a dialog
without requiring login; missing or unavailable QR images show a clear message.


Publishing requires saving details, uploading at least one product image, then
uploading a separate WeChat contact QR image. Product details provide contact
preview and offline reservation. Sellers confirm delivery (`pending_handover` →
`pending_receipt`), then buyers confirm receipt to complete the transaction. Only
reservations awaiting seller delivery can expire or be cancelled by either the
buyer or the seller. There is no
payment action.

## Displayed identifiers

Book details and order participants show public usernames from the API, never
synthetic labels such as “读者 #1”. Order cards use book titles rather than
internal book IDs. Missing names use a role or a clear unavailable-name message.
User-facing errors omit diagnostic request IDs; `ApiError.requestId` and request
headers retain them for troubleshooting.

Order numbers remain visible for transaction lookup. The admin book-number
input remains available to locate the target of moderation, with an explicit
label and confirmation. Image positions, form steps, prices, dates and counts
are functional values and remain unchanged. Internal IDs are still used in
requests, route parameters and Vue keys; hiding them is not an authorization
mechanism. Backend ownership and participant checks continue to use trusted IDs.
