-- Reset existing development data before applying this migration.
-- Historical migration files retain their checksums. the current schema has no payment fields.
ALTER TABLE orders
    DROP CHECK chk_orders_status,
    DROP COLUMN paid_at,
    MODIFY COLUMN active_book_id BIGINT UNSIGNED GENERATED ALWAYS AS (
        CASE WHEN status IN ('pending_handover', 'completed') THEN book_id ELSE NULL END
    ) STORED,
    ADD CONSTRAINT chk_orders_status CHECK (status IN ('pending_handover', 'completed', 'cancelled', 'expired'));
