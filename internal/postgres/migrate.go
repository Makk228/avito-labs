package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/Makk228/avito-labs/migrations"
)

func Migrate(ctx context.Context, pool *pgxpool.Pool, command string, log *slog.Logger) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	switch command {
	case "up":
		results, err := provider.Up(ctx)
		logResults(log, results...)
		if err != nil {
			return fmt.Errorf("migrate up: %w", err)
		}
		if len(results) == 0 {
			log.Info("no migrations to apply")
		}
	case "down":
		result, err := provider.Down(ctx)
		if errors.Is(err, goose.ErrNoNextVersion) {
			log.Info("no migrations to roll back")
			return nil
		}
		if result != nil {
			logResults(log, result)
		}
		if err != nil {
			return fmt.Errorf("migrate down: %w", err)
		}
	case "reset":
		results, err := provider.DownTo(ctx, 0)
		logResults(log, results...)
		if err != nil {
			return fmt.Errorf("migrate reset: %w", err)
		}
		if len(results) == 0 {
			log.Info("no migrations to roll back")
		}
	case "status":
		statuses, err := provider.Status(ctx)
		if err != nil {
			return fmt.Errorf("migrate status: %w", err)
		}
		for _, s := range statuses {
			attrs := []any{
				slog.Int64("version", s.Source.Version),
				slog.String("file", s.Source.Path),
				slog.String("state", string(s.State)),
			}
			if !s.AppliedAt.IsZero() {
				attrs = append(attrs, slog.Time("applied_at", s.AppliedAt))
			}
			log.Info("migration", attrs...)
		}
	default:
		return fmt.Errorf("unknown migrate command %q, want one of: up, down, reset, status", command)
	}
	return nil
}

func logResults(log *slog.Logger, results ...*goose.MigrationResult) {
	for _, r := range results {
		if r.Error != nil {
			continue
		}
		msg := "migration applied"
		if r.Direction == "down" {
			msg = "migration rolled back"
		}
		log.Info(msg,
			slog.Int64("version", r.Source.Version),
			slog.String("file", r.Source.Path),
			slog.String("duration", r.Duration.Round(time.Millisecond).String()),
		)
	}
}
