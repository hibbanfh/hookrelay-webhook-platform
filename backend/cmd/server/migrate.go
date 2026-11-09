package main

import (
	"errors"
	"fmt"
	"log/slog"

	"hookrelay/internal/platform/postgres"
	"hookrelay/migrations"
)

// runMigrate applies all pending migrations (up) or reverts one step (down).
func runMigrate(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: hookrelay migrate up|down")
	}
	dir := postgres.Direction(args[0])
	if dir != postgres.Up && dir != postgres.Down {
		return fmt.Errorf("unknown migrate direction %q (want up|down)", args[0])
	}
	cfg, log, err := loadConfig("migrate")
	if err != nil {
		return err
	}
	if err := postgres.Migrate(cfg.DatabaseURL, migrations.Files, dir); err != nil {
		return err
	}
	log.Info("migrations applied", slog.String("direction", string(dir)))
	return nil
}
