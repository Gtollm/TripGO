package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrIdempotencyKeyExists = errors.New("idempotency key already exists")

type IdempotencyKey struct {
	Key         uuid.UUID
	RequestHash string
	TripID      uuid.UUID
	CreatedAt   time.Time
}

type IdempotencyRepository interface {
	DeleteExpired(ctx context.Context, key uuid.UUID, expiredBefore time.Time) error
	Create(ctx context.Context, key IdempotencyKey) error
	Attach(ctx context.Context, key, tripID uuid.UUID) error
	Get(ctx context.Context, key uuid.UUID) (IdempotencyKey, error)
}