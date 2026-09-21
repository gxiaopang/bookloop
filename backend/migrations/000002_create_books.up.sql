CREATE TABLE books (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    seller_id BIGINT UNSIGNED NOT NULL,
    title VARCHAR(200) NOT NULL,
    description TEXT NULL,
    price DECIMAL(10,2) NOT NULL,
    `condition` VARCHAR(16) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'off_shelf',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    CONSTRAINT fk_books_seller FOREIGN KEY (seller_id) REFERENCES users (id) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT chk_books_price CHECK (price > 0),
    CONSTRAINT chk_books_condition CHECK (`condition` IN ('new', 'like_new', 'good', 'fair')),
    CONSTRAINT chk_books_status CHECK (status IN ('available', 'reserved', 'sold', 'off_shelf', 'blocked')),
    KEY ix_books_status_created_id (status, created_at, id),
    KEY ix_books_status_price_id (status, price, id),
    KEY ix_books_status_condition_created_id (status, `condition`, created_at, id),
    KEY ix_books_status_condition_price_id (status, `condition`, price, id),
    KEY ix_books_seller_status_created (seller_id, status, created_at)
) ENGINE=InnoDB DEFAULT CHARACTER SET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE book_images (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    book_id BIGINT UNSIGNED NOT NULL,
    image_url VARCHAR(2048) NOT NULL,
    sort_order SMALLINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    CONSTRAINT fk_book_images_book FOREIGN KEY (book_id) REFERENCES books (id) ON UPDATE RESTRICT ON DELETE CASCADE,
    CONSTRAINT chk_book_images_sort_order CHECK (sort_order > 0),
    UNIQUE KEY ux_book_images_book_sort (book_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARACTER SET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
