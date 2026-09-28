-- +goose Up
CREATE TABLE idempotency_keys (
    idempotency_key UUID PRIMARY KEY,
    request_hash    TEXT NOT NULL,
    trip_id         UUID REFERENCES trips(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,

    CHECK (expires_at > created_at)
);

CREATE INDEX idempotency_keys_expires_at_idx ON idempotency_keys (expires_at);

-- +goose Down
DROP TABLE idempotency_keys;
