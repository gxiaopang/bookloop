-- Only roll back after clearing development orders. old application code cannot read pending_handover.
ALTER TABLE orders
    DROP CHECK chk_orders_status,
    ADD COLUMN paid_at DATETIME(3) NULL AFTER expires_at,
    MODIFY COLUMN active_book_id BIGINT UNSIGNED GENERATED ALWAYS AS (
        CASE WHEN status IN ('pending_payment', 'paid', 'completed') THEN book_id ELSE NULL END
    ) STORED,
    ADD CONSTRAINT chk_orders_status CHECK (status IN ('pending_payment', 'paid', 'completed', 'cancelled', 'expired'));
