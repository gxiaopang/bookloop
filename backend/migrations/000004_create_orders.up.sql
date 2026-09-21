CREATE TABLE orders (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    order_no CHAR(26) NOT NULL,
    book_id BIGINT UNSIGNED NOT NULL,
    buyer_id BIGINT UNSIGNED NOT NULL,
    seller_id BIGINT UNSIGNED NOT NULL,
    book_title VARCHAR(200) NOT NULL,
    book_price DECIMAL(10,2) NOT NULL,
    status VARCHAR(24) NOT NULL,
    idempotency_key VARCHAR(64) NOT NULL,
    request_fingerprint CHAR(64) NOT NULL,
    expires_at DATETIME(3) NOT NULL,
    paid_at DATETIME(3) NULL,
    completed_at DATETIME(3) NULL,
    cancelled_at DATETIME(3) NULL,
    cancel_reason VARCHAR(255) NULL,
    active_book_id BIGINT UNSIGNED GENERATED ALWAYS AS (
        CASE
            WHEN status IN ('pending_payment', 'paid', 'completed') THEN book_id
            WHEN status IN ('cancelled', 'expired') THEN NULL
        END
    ) STORED,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    CONSTRAINT fk_orders_book FOREIGN KEY (book_id) REFERENCES books (id) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT fk_orders_buyer FOREIGN KEY (buyer_id) REFERENCES users (id) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT fk_orders_seller FOREIGN KEY (seller_id) REFERENCES users (id) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT chk_orders_distinct_participants CHECK (buyer_id <> seller_id),
    CONSTRAINT chk_orders_book_price CHECK (book_price > 0),
    CONSTRAINT chk_orders_status CHECK (status IN ('pending_payment', 'paid', 'completed', 'cancelled', 'expired')),
    UNIQUE KEY ux_orders_order_no (order_no),
    UNIQUE KEY ux_orders_buyer_idempotency (buyer_id, idempotency_key),
    UNIQUE KEY ux_orders_active_book (active_book_id),
    KEY ix_orders_buyer_created_id (buyer_id, created_at, id),
    KEY ix_orders_seller_created_id (seller_id, created_at, id),
    KEY ix_orders_book_status (book_id, status),
    KEY ix_orders_status_expires (status, expires_at)
) ENGINE=InnoDB DEFAULT CHARACTER SET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
