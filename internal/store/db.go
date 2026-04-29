package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shuosc/scnet-server/config"
)

const (
	initialMigrationFilename = "001_init.sql"
	schemaMigrationsTable    = "schema_migrations"
)

func NewPool(cfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(context.Background(), cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

func ResolveMigrationsDir(migrationsDir string) (string, error) {
	var candidates []string
	seen := make(map[string]struct{})

	addCandidate := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		candidates = append(candidates, path)
	}

	addCandidate(migrationsDir)
	addCandidate("/etc/scnet/migrations")
	if executablePath, err := os.Executable(); err == nil {
		addCandidate(filepath.Join(filepath.Dir(executablePath), "migrations"))
	}

	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("migration directory not found; checked: %s", strings.Join(candidates, ", "))
}

func RunMigrations(pool *pgxpool.Pool, migrationsDir string) error {
	resolvedDir, err := ResolveMigrationsDir(migrationsDir)
	if err != nil {
		return err
	}

	ctx := context.Background()
	if err := ensureSchemaMigrationsTable(ctx, pool); err != nil {
		return fmt.Errorf("create schema migrations table: %w", err)
	}

	applied, err := listAppliedMigrations(ctx, pool)
	if err != nil {
		return fmt.Errorf("list applied migrations: %w", err)
	}

	entries, err := os.ReadDir(resolvedDir)
	if err != nil {
		return fmt.Errorf("read migrations dir %q: %w", resolvedDir, err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}

		filename := entry.Name()
		if applied[filename] {
			continue
		}
		if filename == initialMigrationFilename {
			legacyApplied, err := maybeMarkLegacyInitApplied(ctx, pool, filename)
			if err != nil {
				return fmt.Errorf("check legacy migration %s: %w", filename, err)
			}
			if legacyApplied {
				applied[filename] = true
				continue
			}
		}

		data, err := os.ReadFile(filepath.Join(resolvedDir, filename))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", filename, err)
		}

		upSQL, err := extractUpMigrationSQL(data)
		if err != nil {
			return fmt.Errorf("parse migration %s: %w", filename, err)
		}
		if err := applyMigration(ctx, pool, filename, upSQL); err != nil {
			return fmt.Errorf("apply migration %s: %w", filename, err)
		}
		applied[filename] = true
	}
	return nil
}

func ensureSchemaMigrationsTable(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	return err
}

func listAppliedMigrations(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx, `SELECT filename FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var filename string
		if err := rows.Scan(&filename); err != nil {
			return nil, err
		}
		applied[filename] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return applied, nil
}

func maybeMarkLegacyInitApplied(ctx context.Context, pool *pgxpool.Pool, filename string) (bool, error) {
	var usersTable *string
	var invitesTable *string
	var peersTable *string

	if err := pool.QueryRow(ctx, `
		SELECT
			to_regclass('public.users'),
			to_regclass('public.invite_codes'),
			to_regclass('public.peers')
	`).Scan(&usersTable, &invitesTable, &peersTable); err != nil {
		return false, err
	}

	if usersTable == nil || invitesTable == nil || peersTable == nil {
		return false, nil
	}

	return true, recordMigration(ctx, pool, filename)
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, filename, upSQL string) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err = tx.Exec(ctx, upSQL); err != nil {
		return err
	}
	if err = recordMigration(ctx, tx, filename); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func recordMigration(ctx context.Context, recorder interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}, filename string) error {
	_, err := recorder.Exec(
		ctx,
		`INSERT INTO schema_migrations (filename) VALUES ($1) ON CONFLICT (filename) DO NOTHING`,
		filename,
	)
	return err
}

func extractUpMigrationSQL(data []byte) (string, error) {
	content := string(data)
	lines := strings.Split(content, "\n")

	var (
		foundUp bool
		upLines []string
	)

	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case "-- +migrate Up":
			foundUp = true
			continue
		case "-- +migrate Down":
			if foundUp {
				upSQL := strings.TrimSpace(strings.Join(upLines, "\n"))
				if upSQL == "" {
					return "", fmt.Errorf("empty Up section")
				}
				return upSQL, nil
			}
		}

		if foundUp {
			upLines = append(upLines, line)
		}
	}

	if foundUp {
		upSQL := strings.TrimSpace(strings.Join(upLines, "\n"))
		if upSQL == "" {
			return "", fmt.Errorf("empty Up section")
		}
		return upSQL, nil
	}

	upSQL := strings.TrimSpace(content)
	if upSQL == "" {
		return "", fmt.Errorf("empty migration file")
	}
	return upSQL, nil
}
