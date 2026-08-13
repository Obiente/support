package store

import (
	"context"
	"embed"
	"fmt"
	"sort"
)

//go:embed migrations/*.sql
var migrations embed.FS

func (postgres *Postgres) Migrate(ctx context.Context) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		migration, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		if _, err := postgres.pool.Exec(ctx, string(migration)); err != nil {
			return fmt.Errorf("apply %s: %w", entry.Name(), err)
		}
	}
	return nil
}
