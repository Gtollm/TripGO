-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS trips_active_stale_poll_idx
    ON trips (COALESCE(last_position_at, started_at), id)
    INCLUDE (driver_id)
    WHERE status = 'active';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS trips_active_stale_poll_idx;
