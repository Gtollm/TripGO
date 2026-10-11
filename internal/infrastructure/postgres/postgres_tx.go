package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresTxManager struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func NewTxManager(pool *pgxpool.Pool, queryTimeout time.Duration) *PostgresTxManager {
	return &PostgresTxManager{pool: pool, queryTimeout: queryTimeout}
}

type txKey struct{}

func (p PostgresTxManager) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}

	beginCtx, cancelBegin := context.WithTimeout(ctx, p.queryTimeout)
	tx, err := p.pool.BeginTx(beginCtx, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	})
	cancelBegin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	ctx = context.WithValue(ctx, txKey{}, tx)

	defer func() {
		rec := recover()
		rollbackCtx, cancelRollback := context.WithTimeout(context.WithoutCancel(ctx), p.queryTimeout)
		defer cancelRollback()
		_ = tx.Rollback(rollbackCtx)
		if rec != nil {
			panic(rec)
		}
	}()

	if err := fn(ctx); err != nil {
		return err
	}
	commitCtx, cancelCommit := context.WithTimeout(ctx, p.queryTimeout)
	defer cancelCommit()
	if err := tx.Commit(commitCtx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
