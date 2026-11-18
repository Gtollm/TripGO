package trip

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/Gtollm/TripGO/internal/domain/trip"
	"github.com/Gtollm/TripGO/internal/infra/postgres/transaction"
	"github.com/Gtollm/TripGO/internal/model"
)

const (
	uniqueViolation            = "23505"
	activeDriverTripConstraint = "trips_driver_id_active_idx"
)

var statement = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

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

func (r *Repository) Create(ctx context.Context, created model.Trip, change model.Transition) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	if err := r.insertTrip(ctx, created, change.At); err != nil {
		return err
	}

	return r.insertHistory(ctx, created.ID, change)
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (model.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()
	return r.get(ctx, id, false)
}

func (r *Repository) GetForUpdate(ctx context.Context, id uuid.UUID) (model.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()
	return r.get(ctx, id, true)
}

func (r *Repository) UpdateStatus(ctx context.Context, updated model.Trip, change model.Transition) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := statement.Update("trips").
		Set("status", updated.Status.String()).
		Set("finished_at", updated.FinishedAt).
		Set("updated_at", change.At).
		Where(sq.Eq{"id": updated.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update trip query: %w", err)
	}

	tag, err := transaction.FromContext(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return mapWriteError("update trip", err)
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrTripNotFound
	}

	return r.insertHistory(ctx, updated.ID, change)
}

func (r *Repository) insertTrip(ctx context.Context, created model.Trip, at time.Time) error {
	query, args, err := statement.Insert("trips").
		Columns(
			"id",
			"user_id",
			"driver_id",
			"start_latitude",
			"start_longitude",
			"end_latitude",
			"end_longitude",
			"price",
			"status",
			"started_at",
			"finished_at",
			"created_at",
			"updated_at",
		).
		Values(
			created.ID,
			created.UserID,
			created.DriverID,
			created.Start.Latitude,
			created.Start.Longitude,
			created.End.Latitude,
			created.End.Longitude,
			created.Price,
			created.Status.String(),
			created.StartedAt,
			created.FinishedAt,
			at,
			at,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip query: %w", err)
	}

	_, err = transaction.FromContext(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return mapWriteError("insert trip", err)
	}

	return nil
}

func (r *Repository) insertHistory(ctx context.Context, tripID uuid.UUID, change model.Transition) error {
	query, args, err := statement.Insert("trip_status_history").
		Columns(
			"trip_id",
			"from_status",
			"to_status",
			"reason",
			"changed_at",
		).
		Values(
			tripID,
			historyStatus(change.From),
			change.To.String(),
			change.Reason,
			change.At,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip status history query: %w", err)
	}

	_, err = transaction.FromContext(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("insert trip status history: %w", err)
	}

	return nil
}

func (r *Repository) get(ctx context.Context, id uuid.UUID, forUpdate bool) (model.Trip, error) {
	query := statement.Select(
		"id",
		"user_id",
		"driver_id",
		"start_latitude",
		"start_longitude",
		"end_latitude",
		"end_longitude",
		"price",
		"status",
		"started_at",
		"finished_at",
	).
		From("trips").
		Where(sq.Eq{"id": id})
	if forUpdate {
		query = query.Suffix("FOR UPDATE")
	}

	sql, args, err := query.ToSql()
	if err != nil {
		return model.Trip{}, fmt.Errorf("build get trip query: %w", err)
	}

	var (
		found      model.Trip
		statusText string
	)
	err = transaction.FromContext(ctx, r.pool).QueryRow(ctx, sql, args...).Scan(
		&found.ID,
		&found.UserID,
		&found.DriverID,
		&found.Start.Latitude,
		&found.Start.Longitude,
		&found.End.Latitude,
		&found.End.Longitude,
		&found.Price,
		&statusText,
		&found.StartedAt,
		&found.FinishedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Trip{}, domain.ErrTripNotFound
	}

	if err != nil {
		return model.Trip{}, fmt.Errorf("get trip: %w", err)
	}

	found.Status, err = domain.ParseStatus(statusText)
	if err != nil {
		return model.Trip{}, fmt.Errorf("parse trip status: %w", err)
	}

	return found, nil
}

func (r *Repository) CountActive(ctx context.Context) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := statement.Select("COUNT(*)").From("trips").Where(sq.Eq{"status": "active"}).ToSql()
	if err != nil {
		return 0, fmt.Errorf("build count active trips: %w", err)
	}

	var count int64
	if err := transaction.FromContext(ctx, r.pool).QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active trips: %w", err)
	}

	return count, nil
}

func historyStatus(status domain.Status) any {
	if status == domain.StatusNONE {
		return nil
	}

	return status.String()
}

func mapWriteError(action string, err error) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == uniqueViolation && pgErr.ConstraintName == activeDriverTripConstraint {
		return domain.ErrDriverBusy
	}

	return fmt.Errorf("%s: %w", action, err)
}
