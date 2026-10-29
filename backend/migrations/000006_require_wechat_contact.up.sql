ALTER TABLE books ADD COLUMN wechat_qr_code_url VARCHAR(2048) NULL AFTER status;

-- Existing listings must supply contact information before being listed again.
UPDATE books SET status = 'off_shelf' WHERE status = 'available' AND wechat_qr_code_url IS NULL;
