package tenant

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"alfred-ai/internal/domain"
)

// SQLiteTenantStore implements domain.TenantStore using SQLite.
type SQLiteTenantStore struct {
	db *sql.DB
}

// NewSQLiteTenantStore opens (or creates) a SQLite database at dbPath
// and runs the schema migration.
func NewSQLiteTenantStore(dbPath string) (*SQLiteTenantStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open tenant db: %w", err)
	}
	// WAL mode for better concurrent reads.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate tenant db: %w", err)
	}
	return &SQLiteTenantStore{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS tenants (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			plan       TEXT NOT NULL DEFAULT 'free',
			config     TEXT NOT NULL DEFAULT '{}',
			limits     TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`)
	return err
}

// Close closes the underlying database connection.
func (s *SQLiteTenantStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteTenantStore) Get(_ context.Context, id string) (*domain.Tenant, error) {
	row := s.db.QueryRow(
		"SELECT id, name, plan, config, limits, created_at, updated_at FROM tenants WHERE id = ?", id,
	)
	return scanTenant(row)
}

func (s *SQLiteTenantStore) Create(_ context.Context, t *domain.Tenant) error {
	cfgJSON, err := json.Marshal(t.Config)
	if err != nil {
		return fmt.Errorf("marshal tenant config: %w", err)
	}
	limJSON, err := json.Marshal(t.Limits)
	if err != nil {
		return fmt.Errorf("marshal tenant limits: %w", err)
	}
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	_, err = s.db.Exec(
		"INSERT INTO tenants (id, name, plan, config, limits, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		t.ID, t.Name, string(t.Plan), string(cfgJSON), string(limJSON),
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteTenantStore) Update(_ context.Context, t *domain.Tenant) error {
	cfgJSON, err := json.Marshal(t.Config)
	if err != nil {
		return fmt.Errorf("marshal tenant config: %w", err)
	}
	limJSON, err := json.Marshal(t.Limits)
	if err != nil {
		return fmt.Errorf("marshal tenant limits: %w", err)
	}
	now := time.Now().UTC()
	t.UpdatedAt = now
	res, err := s.db.Exec(
		"UPDATE tenants SET name = ?, plan = ?, config = ?, limits = ?, updated_at = ? WHERE id = ?",
		t.Name, string(t.Plan), string(cfgJSON), string(limJSON),
		now.Format(time.RFC3339Nano), t.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.ErrTenantNotFound
	}
	return nil
}

func (s *SQLiteTenantStore) Delete(_ context.Context, id string) error {
	res, err := s.db.Exec("DELETE FROM tenants WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.ErrTenantNotFound
	}
	return nil
}

func (s *SQLiteTenantStore) List(_ context.Context) ([]*domain.Tenant, error) {
	rows, err := s.db.Query("SELECT id, name, plan, config, limits, created_at, updated_at FROM tenants ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tenants []*domain.Tenant
	for rows.Next() {
		t, err := scanTenantRows(rows)
		if err != nil {
			return nil, err
		}
		tenants = append(tenants, t)
	}
	return tenants, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTenant(row *sql.Row) (*domain.Tenant, error) {
	var t domain.Tenant
	var plan, cfgStr, limStr, createdStr, updatedStr string
	if err := row.Scan(&t.ID, &t.Name, &plan, &cfgStr, &limStr, &createdStr, &updatedStr); err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrTenantNotFound
		}
		return nil, err
	}
	t.Plan = domain.TenantPlan(plan)
	if err := json.Unmarshal([]byte(cfgStr), &t.Config); err != nil {
		return nil, fmt.Errorf("unmarshal tenant config: %w", err)
	}
	if err := json.Unmarshal([]byte(limStr), &t.Limits); err != nil {
		return nil, fmt.Errorf("unmarshal tenant limits: %w", err)
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)
	return &t, nil
}

func scanTenantRows(rows *sql.Rows) (*domain.Tenant, error) {
	var t domain.Tenant
	var plan, cfgStr, limStr, createdStr, updatedStr string
	if err := rows.Scan(&t.ID, &t.Name, &plan, &cfgStr, &limStr, &createdStr, &updatedStr); err != nil {
		return nil, err
	}
	t.Plan = domain.TenantPlan(plan)
	if err := json.Unmarshal([]byte(cfgStr), &t.Config); err != nil {
		return nil, fmt.Errorf("unmarshal tenant config: %w", err)
	}
	if err := json.Unmarshal([]byte(limStr), &t.Limits); err != nil {
		return nil, fmt.Errorf("unmarshal tenant limits: %w", err)
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)
	return &t, nil
}
