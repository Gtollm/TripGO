package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	"github.com/Gtollm/TripGO/internal/infrastructure"
	"github.com/Gtollm/TripGO/internal/repository"
)

type Service struct {
	trips repository.TripRepository
	tx    infrastructure.TxManager
}

func New(trips repository.TripRepository, tx infrastructure.TxManager) *Service {
	return &Service{
		trips: trips,
		tx:    tx,
	}
}

func (s *Service) Create(
	ctx context.Context,
	userID, driverID uuid.UUID,
	start, end trip.Coordinates,
	price int64,
) (trip.Trip, error) {
	created, change, err := trip.New(uuid.New(), userID, driverID, start, end, price, time.Now())
	if err != nil {
		return trip.Trip{}, err
	}

	err = s.tx.Do(
		ctx, func(ctx context.Context) error {
			return s.trips.Create(ctx, created, change)
		},
	)
	if err != nil {
		return trip.Trip{}, err
	}
	return created, nil
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