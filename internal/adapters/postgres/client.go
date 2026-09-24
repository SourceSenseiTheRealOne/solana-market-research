package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("database URL is required")
	}
	return pgxpool.New(ctx, databaseURL)
}

func OpenEnt(ctx context.Context, databaseURL string) (*ent.Client, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("database URL is required")
	}

	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	database := sql.OpenDB(stdlib.GetConnector(*config))
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, err
	}
	return ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, database))), nil
}
