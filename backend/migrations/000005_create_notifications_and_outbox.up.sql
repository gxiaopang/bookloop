CREATE TABLE outbox_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    aggregate_type VARCHAR(32) NOT NULL,
    aggregate_id VARCHAR(64) NOT NULL,
    event_type VARCHAR(64) NOT NULL,
    payload JSON NOT NULL,
    occurred_at DATETIME(3) NOT NULL,
    available_at DATETIME(3) NOT NULL,
    published_at DATETIME(3) NULL,
    attempts INT UNSIGNED NOT NULL DEFAULT 0,
    next_attempt_at DATETIME(3) NULL,
    locked_by VARCHAR(64) NULL,
    locked_at DATETIME(3) NULL,
    last_error VARCHAR(500) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY ix_outbox_publishable (published_at, available_at, next_attempt_at, id),
    KEY ix_outbox_locked (locked_at, id),
    KEY ix_outbox_aggregate (aggregate_type, aggregate_id, id)
) ENGINE=InnoDB DEFAULT CHARACTER SET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE processed_messages (
    consumer_name VARCHAR(64) NOT NULL,
    event_id BIGINT UNSIGNED NOT NULL,
    processed_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (consumer_name, event_id),
    CONSTRAINT fk_processed_messages_event FOREIGN KEY (event_id)
        REFERENCES outbox_events (id) ON UPDATE RESTRICT ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARACTER SET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE notifications (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    event_id BIGINT UNSIGNED NOT NULL,
    type VARCHAR(64) NOT NULL,
    title VARCHAR(100) NOT NULL,
    content VARCHAR(500) NOT NULL,
    order_no CHAR(26) NULL,
    read_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    CONSTRAINT fk_notifications_user FOREIGN KEY (user_id)
        REFERENCES users (id) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT fk_notifications_event FOREIGN KEY (event_id)
        REFERENCES outbox_events (id) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT fk_notifications_order FOREIGN KEY (order_no)
        REFERENCES orders (order_no) ON UPDATE RESTRICT ON DELETE RESTRICT,
    UNIQUE KEY ux_notifications_user_event (user_id, event_id),
    KEY ix_notifications_user_created (user_id, created_at, id),
    KEY ix_notifications_user_read (user_id, read_at, created_at, id)
) ENGINE=InnoDB DEFAULT CHARACTER SET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
