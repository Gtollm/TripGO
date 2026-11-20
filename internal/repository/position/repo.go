package position

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Gtollm/TripGO/internal/infra/postgres/transaction"
	"github.com/Gtollm/TripGO/internal/model"
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

func (r *Repository) Create(ctx context.Context, pos model.Position, createdAt time.Time) (model.Position, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := statement.Insert("trip_positions").
		Columns("trip_id", "latitude", "longitude", "recorded_at", "created_at").
		Values(
			pos.TripID,
			pos.Coordinates.Latitude,
			pos.Coordinates.Longitude,
			pos.RecordedAt,
			createdAt,
		).
		Suffix("RETURNING id").
		ToSql()
	if err != nil {
		return model.Position{}, fmt.Errorf("build insert trip position query: %w", err)
	}

	if err := transaction.FromContext(ctx, r.pool).QueryRow(ctx, query, args...).Scan(&pos.ID); err != nil {
		return model.Position{}, fmt.Errorf("insert trip position: %w", err)
	}

	return pos, nil
}

func (r *Repository) ListByTrip(ctx context.Context, tripID uuid.UUID) ([]model.Position, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := statement.Select("id", "trip_id", "latitude", "longitude", "recorded_at").
		From("trip_positions").
		Where(sq.Eq{"trip_id": tripID}).
		OrderBy("recorded_at ASC", "id ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list trip positions query: %w", err)
	}

	rows, err := transaction.FromContext(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list trip positions: %w", err)
	}

	positions, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Position, error) {
		var pos model.Position
		err := row.Scan(
			&pos.ID,
			&pos.TripID,
			&pos.Coordinates.Latitude,
			&pos.Coordinates.Longitude,
			&pos.RecordedAt,
		)
		return pos, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan trip positions: %w", err)
	}

	return positions, nil
}
