package idempotency

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	"github.com/Gtollm/TripGO/internal/infra/postgres/transaction"
	"github.com/Gtollm/TripGO/internal/model"
)

var statement = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

const (
	uniqueViolation          = "23505"
	idempotencyKeyConstraint = "idempotency_keys_pkey"
	idempotencyKeyColumn     = "idempotency_key"
)

type Repository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func New(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	return &Repository{
		pool:         pool,
		queryTimeout: queryTimeout,
	}
}

func (r *Repository) DeleteExpired(
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

	_, err = transaction.FromContext(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete expired idempotency key: %w", err)
	}

	return nil
}

func (r *Repository) Create(ctx context.Context, key model.IdempotencyKey) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := statement.Insert("idempotency_keys").
		Columns(idempotencyKeyColumn, "request_hash", "created_at").
		Values(key.Key, key.RequestHash, key.CreatedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert idempotency key query: %w", err)
	}

	_, err = transaction.FromContext(ctx, r.pool).Exec(ctx, query, args...)
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == uniqueViolation && pgErr.ConstraintName == idempotencyKeyConstraint {
		return trip.ErrIdempotencyKeyExists
	}

	if err != nil {
		return fmt.Errorf("insert idempotency key: %w", err)
	}

	return nil
}

func (r *Repository) Attach(ctx context.Context, key, tripID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := statement.Update("idempotency_keys").
		Set("trip_id", tripID).
		Where(sq.Eq{idempotencyKeyColumn: key}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build attach idempotency key query: %w", err)
	}

	tag, err := transaction.FromContext(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("attach idempotency key: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("attach idempotency key %s: key not found", key)
	}

	return nil
}

func (r *Repository) Get(ctx context.Context, key uuid.UUID) (model.IdempotencyKey, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := statement.Select(idempotencyKeyColumn, "request_hash", "trip_id", "created_at").
		From("idempotency_keys").
		Where(sq.Eq{idempotencyKeyColumn: key}).
		ToSql()
	if err != nil {
		return model.IdempotencyKey{}, fmt.Errorf("build get idempotency key query: %w", err)
	}

	var (
		stored model.IdempotencyKey
		tripID *uuid.UUID
	)
	err = transaction.FromContext(ctx, r.pool).QueryRow(ctx, query, args...).Scan(
		&stored.Key,
		&stored.RequestHash,
		&tripID,
		&stored.CreatedAt,
	)
	if err != nil {
		return model.IdempotencyKey{}, fmt.Errorf("get idempotency key: %w", err)
	}

	if tripID == nil {
		return model.IdempotencyKey{}, fmt.Errorf("idempotency key %s has no trip", key)
	}

	stored.TripID = *tripID
	return stored, nil
}
