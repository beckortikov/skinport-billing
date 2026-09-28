-- All money values are stored in minor units (cents).

CREATE TABLE IF NOT EXISTS users (
    id      BIGINT PRIMARY KEY,
    balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0)
);

CREATE TABLE IF NOT EXISTS withdrawals (
    id             BIGSERIAL PRIMARY KEY,
    user_id        BIGINT      NOT NULL REFERENCES users (id),
    amount         BIGINT      NOT NULL CHECK (amount > 0),
    balance_before BIGINT      NOT NULL,
    balance_after  BIGINT      NOT NULL CHECK (balance_after >= 0),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (balance_after = balance_before - amount)
);

CREATE INDEX IF NOT EXISTS withdrawals_user_id_id_idx ON withdrawals (user_id, id DESC);

INSERT INTO users (id, balance) VALUES (1, 100000) ON CONFLICT (id) DO NOTHING;
