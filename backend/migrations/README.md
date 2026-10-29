# Database migration convention

BookLoop uses explicit, reviewable SQL migrations. Application startup must not call
GORM `AutoMigrate` for business tables.

Migrations use monotonically increasing, zero-padded pairs:

```text
000001_create_users.up.sql
000001_create_users.down.sql
```

Each `up` file applies one coherent schema change and its matching `down` file
documents the safe rollback when one exists. Migrations will be run by an explicit
one-shot command or deployment step, not concurrently by every backend process.

The `cmd/migrate` command discovers `*.up.sql` files, takes a MySQL advisory
lock, applies pending versions in order, and records their SHA-256 checksums in
`schema_migrations`. An already-applied migration must never be edited; add a
new migration instead. MySQL DDL may commit implicitly, so a failed migration
must be inspected and repaired before retrying.

Apply `down` files manually in reverse numeric order only as part of a reviewed
rollback. Phase 2 introduced `users`; Phase 3 introduced `books` and
`book_images`; Phase 4 introduced `favorites`; Phase 5 introduced `orders`;
the messaging enhancement introduced `outbox_events`, `processed_messages`, and
`notifications`.

`000006_require_wechat_contact` adds the separate WeChat contact image URL to
`books`. Existing available listings are taken off shelf until the seller uploads
a contact QR image and explicitly republishes. Reserved and sold books are not
changed. On cancellation or expiration, legacy reservations without contact
information return to `off_shelf`. Rollback drops the new column but does not
automatically relist books. Apply this migration before starting the updated backend.

`000007_use_offline_orders` requires empty development orders. It removes
`paid_at` and replaces payment states with `pending_handover`, retaining the
unique active-book constraint for pending handovers and completed sales. Reset
the authorized development environment before applying it; it deliberately
does not convert legacy payment orders. Rollback also requires empty orders.
