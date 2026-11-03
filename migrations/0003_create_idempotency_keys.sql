-- +goose Up
CREATE TABLE idempotency_keys (
    idempotency_key UUID PRIMARY KEY,
    request_hash    TEXT NOT NULL,
    trip_id         UUID REFERENCES trips(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE idempotency_keys;
