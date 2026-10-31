-- Finish all pending_receipt orders before rollback. The CHECK deliberately
-- rejects rollback while any remain; never release or silently complete them.
ALTER TABLE orders
    DROP CHECK chk_orders_status,
    DROP COLUMN handed_over_at,
    MODIFY COLUMN active_book_id BIGINT UNSIGNED GENERATED ALWAYS AS (
        CASE WHEN status IN ('pending_handover', 'completed') THEN book_id ELSE NULL END
    ) STORED,
    ADD CONSTRAINT chk_orders_status CHECK (status IN ('pending_handover', 'completed', 'cancelled', 'expired'));
