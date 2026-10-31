-- Preserve existing orders, including completed orders without a handover timestamp.
ALTER TABLE orders
    DROP CHECK chk_orders_status,
    ADD COLUMN handed_over_at DATETIME(3) NULL AFTER expires_at,
    MODIFY COLUMN active_book_id BIGINT UNSIGNED GENERATED ALWAYS AS (
        CASE WHEN status IN ('pending_handover', 'pending_receipt', 'completed') THEN book_id ELSE NULL END
    ) STORED,
    ADD CONSTRAINT chk_orders_status CHECK (status IN ('pending_handover', 'pending_receipt', 'completed', 'cancelled', 'expired'));
