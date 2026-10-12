package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Gtollm/TripGO/internal/infrastructure/postgres"
)

const (
	idempotencyKeyConstraint = "idempotency_keys_pkey"
	idempotencyKeyColumn     = "idempotency_key"
)

type PostgresIdempotencyRepository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func NewPostgresIdempotencyRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *PostgresIdempotencyRepository {
	return &PostgresIdempotencyRepository{
		pool:         pool,
		queryTimeout: queryTimeout,
	}
}

func (r *PostgresIdempotencyRepository) DeleteExpired(
	ctx context.Context, key uuid.UUID, expiredBefore time.Time,
) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := statement.Delete("idempotency_keys").
		Where(
			sq.And{
				sq.Eq{idempotencyKeyColumn: key},
				sq.LtOrEq{"created_at": expiredBefore},
			},
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("build delete expired idempotency key query: %w", err)
	}

	_, err = postgres.FromContext(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete expired idempotency key: %w", err)
	}
	return nil
}

func (r *PostgresIdempotencyRepository) Create(ctx context.Context, key IdempotencyKey) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := statement.Insert("idempotency_keys").
		Columns(idempotencyKeyColumn, "request_hash", "created_at").
		Values(key.Key, key.RequestHash, key.CreatedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert idempotency key query: %w", err)
	}

	_, err = postgres.FromContext(ctx, r.pool).Exec(ctx, query, args...)
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == uniqueViolation && pgErr.ConstraintName == idempotencyKeyConstraint {
		return ErrIdempotencyKeyExists
	}
	if err != nil {
		return fmt.Errorf("insert idempotency key: %w", err)
	}
	return nil
}

func (r *PostgresIdempotencyRepository) Attach(ctx context.Context, key, tripID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := statement.Update("idempotency_keys").
		Set("trip_id", tripID).
		Where(sq.Eq{idempotencyKeyColumn: key}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build attach idempotency key query: %w", err)
	}

	tag, err := postgres.FromContext(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("attach idempotency key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("attach idempotency key %s: key not found", key)
	}
	return nil
}

func (r *PostgresIdempotencyRepository) Get(ctx context.Context, key uuid.UUID) (IdempotencyKey, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := statement.Select(idempotencyKeyColumn, "request_hash", "trip_id", "created_at").
		From("idempotency_keys").
		Where(sq.Eq{idempotencyKeyColumn: key}).
		ToSql()
	if err != nil {
		return IdempotencyKey{}, fmt.Errorf("build get idempotency key query: %w", err)
	}

	var (
		stored IdempotencyKey
		tripID *uuid.UUID
	)
	err = postgres.FromContext(ctx, r.pool).QueryRow(ctx, query, args...).Scan(
		&stored.Key,
		&stored.RequestHash,
		&tripID,
		&stored.CreatedAt,
	)
	if err != nil {
		return IdempotencyKey{}, fmt.Errorf("get idempotency key: %w", err)
	}
	if tripID == nil {
		return IdempotencyKey{}, fmt.Errorf("idempotency key %s has no trip", key)
	}

	stored.TripID = *tripID
	return stored, nil
}
