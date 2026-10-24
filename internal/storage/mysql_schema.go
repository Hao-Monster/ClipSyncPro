package storage

const schemaSQL = `
CREATE TABLE IF NOT EXISTS clipboard_record (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    content_type VARCHAR(32) NOT NULL,
    content_text LONGTEXT NULL,
    image_mime VARCHAR(64) NULL,
    image_blob LONGBLOB NULL,
    content_cipher LONGTEXT NULL,
    content_hash CHAR(64) NOT NULL,
    is_sensitive TINYINT(1) NOT NULL DEFAULT 0,
    sensitive_type VARCHAR(32) NULL,
    account_cipher LONGTEXT NULL,
    password_cipher LONGTEXT NULL,
    remark_cipher LONGTEXT NULL,
    search_index LONGTEXT NULL,
    source_device_id VARCHAR(64) NOT NULL,
    source_platform VARCHAR(32) NOT NULL,
    client_created_at DATETIME(3) NOT NULL,
    server_created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NOT NULL,
    deleted_at DATETIME(3) NULL,
    is_favorite TINYINT(1) NOT NULL DEFAULT 0,
    favorite_at DATETIME(3) NULL,
    sync_status VARCHAR(32) NOT NULL DEFAULT 'pending',
    INDEX idx_user_created_at (user_id, client_created_at),
    INDEX idx_hash (content_hash),
    INDEX idx_sensitive (user_id, is_sensitive, sensitive_type),
    INDEX idx_favorite (user_id, is_favorite, favorite_at),
    INDEX idx_sync_status (sync_status)
);

CREATE TABLE IF NOT EXISTS prompt_template (
    id VARCHAR(64) PRIMARY KEY,
    title VARCHAR(128) NOT NULL,
    category VARCHAR(64) NOT NULL DEFAULT '通用',
    template_content LONGTEXT NOT NULL,
    variables_json LONGTEXT NULL,
    shortcut VARCHAR(64) NULL,
    usage_count INT NOT NULL DEFAULT 0,
    is_favorite TINYINT(1) NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    INDEX idx_prompts_category (category),
    INDEX idx_prompts_usage (usage_count, updated_at)
);

CREATE TABLE IF NOT EXISTS sync_queue_v2 (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    record_id VARCHAR(64) NOT NULL,
    action VARCHAR(32) NOT NULL,
    retry_count INT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    last_error TEXT NULL,
    next_attempt_at DATETIME(3) NOT NULL,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    UNIQUE INDEX uq_sync_queue_record (record_id),
    INDEX idx_sync_queue_due (status, next_attempt_at, created_at),
    INDEX idx_sync_queue_schedule (status, retry_count, next_attempt_at, created_at)
);`
