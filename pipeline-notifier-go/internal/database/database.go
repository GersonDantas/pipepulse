package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	configuration, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	configuration.MaxConns = 10
	configuration.MinConns = 1
	configuration.MaxConnLifetime = 30 * time.Minute
	configuration.MaxConnIdleTime = 5 * time.Minute
	configuration.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	return pool, nil
}

func Migrate(ctx context.Context, databaseURL string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open PostgreSQL for migrations: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to PostgreSQL for migrations: %w", err)
	}

	folder, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("load embedded migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, folder, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
