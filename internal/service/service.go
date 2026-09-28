package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	"github.com/Gtollm/TripGO/internal/infrastructure"
	"github.com/Gtollm/TripGO/internal/repository"
)

type Service struct {
	trips  repository.TripRepository
	keys   repository.IdempotencyRepository
	tx     infrastructure.TxManager
	keyTTL time.Duration
}

func New(
	trips repository.TripRepository,
	keys repository.IdempotencyRepository,
	tx infrastructure.TxManager,
	keyTTL time.Duration,
) *Service {
	return &Service{
		trips:  trips,
		keys:   keys,
		tx:     tx,
		keyTTL: keyTTL,
	}
}

func (s *Service) Create(
	ctx context.Context,
	idempotencyKey *uuid.UUID,
	userID, driverID uuid.UUID,
	start, end trip.Coordinates,
	price int64,
) (trip.Trip, bool, error) {
	now := time.Now()
	created, change, err := trip.New(uuid.New(), userID, driverID, start, end, price, now)
	if err != nil {
		return trip.Trip{}, false, err
	}

	result := created
	repeated := false
	err = s.tx.Do(
		ctx, func(ctx context.Context) error {
			if idempotencyKey == nil {
				return s.trips.Create(ctx, created, change)
			}

			hash := requestHash(created)
			if err := s.keys.DeleteExpired(ctx, *idempotencyKey, now.Add(-s.keyTTL)); err != nil {
				return err
			}
			if err := s.keys.Create(ctx, repository.IdempotencyKey{Key: *idempotencyKey, RequestHash: hash, CreatedAt: now}); err != nil {
				return err
			}
			if err := s.trips.Create(ctx, created, change); err != nil {
				return err
			}
			return s.keys.Attach(ctx, *idempotencyKey, created.ID)
		},
	)
	if errors.Is(err, repository.ErrIdempotencyKeyExists) {
		result, err = s.findRepeated(context.WithoutCancel(ctx), *idempotencyKey, requestHash(created))
		repeated = true
	}
	if err != nil {
		return trip.Trip{}, false, err
	}
	return result, repeated, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	return s.trips.Get(ctx, id)
}

func (s *Service) Finish(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	var finished trip.Trip
	err := s.tx.Do(
		ctx, func(ctx context.Context) error {
			found, err := s.trips.GetForUpdate(ctx, id)
			if err != nil {
				return err
			}

			change, err := found.Finish(time.Now())
			if err != nil {
				return err
			}
			if err := s.trips.UpdateStatus(ctx, found, change); err != nil {
				return err
			}

			finished = found
			return nil
		},
	)
	if err != nil {
		return trip.Trip{}, err
	}
	return finished, nil
}

func (s *Service) findRepeated(ctx context.Context, key uuid.UUID, hash string) (trip.Trip, error) {
	stored, err := s.keys.Get(ctx, key)
	if err != nil {
		return trip.Trip{}, err
	}
	if stored.RequestHash != hash {
		return trip.Trip{}, trip.ErrIdempotencyConflict
	}
	return s.trips.Get(ctx, stored.TripID)
}

func requestHash(t trip.Trip) string {
	fields := []string{
		t.UserID.String(),
		t.DriverID.String(),
		strconv.FormatFloat(t.Start.Latitude, 'g', -1, 64),
		strconv.FormatFloat(t.Start.Longitude, 'g', -1, 64),
		strconv.FormatFloat(t.End.Latitude, 'g', -1, 64),
		strconv.FormatFloat(t.End.Longitude, 'g', -1, 64),
		strconv.FormatInt(t.Price, 10),
	}
	sum := sha256.Sum256([]byte(strings.Join(fields, "|")))
	return hex.EncodeToString(sum[:])
}
