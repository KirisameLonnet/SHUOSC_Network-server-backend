-- +migrate Up
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    student_id VARCHAR(20) UNIQUE NOT NULL,
    password   VARCHAR(255) NOT NULL,
    role       VARCHAR(8) DEFAULT 'user',
    invite_id  UUID,
    display_name VARCHAR(64),
    email      VARCHAR(255),
    phone      VARCHAR(32),
    wechat     VARCHAR(64),
    telegram   VARCHAR(64),
    max_peers  INT DEFAULT 5,
    status     VARCHAR(16) DEFAULT 'active',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE invite_codes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code       VARCHAR(32) UNIQUE NOT NULL,
    created_by UUID,
    used_by    UUID,
    max_uses   INT DEFAULT 1,
    use_count  INT DEFAULT 0,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE peers (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID REFERENCES users(id) NOT NULL,
    public_key VARCHAR(64) UNIQUE NOT NULL,
    assigned_ip INET NOT NULL,
    status     VARCHAR(16) DEFAULT 'active',
    last_seen  TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_users_student_id ON users(student_id);
CREATE INDEX idx_peers_user_id ON peers(user_id);
CREATE INDEX idx_peers_public_key ON peers(public_key);
CREATE INDEX idx_peers_user_status ON peers(user_id, status);
CREATE INDEX idx_invite_codes_code ON invite_codes(code);

-- +migrate Down
DROP TABLE IF EXISTS peers;
DROP TABLE IF EXISTS invite_codes;
DROP TABLE IF EXISTS users;
