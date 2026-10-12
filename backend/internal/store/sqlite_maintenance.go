package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (s *SQLite) CreateMaintenance(ctx context.Context, m Maintenance) (Maintenance, error) {
	m.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
	m.StartsAt = m.StartsAt.UTC().Truncate(time.Millisecond)
	m.EndsAt = m.EndsAt.UTC().Truncate(time.Millisecond)

	var monitorID sql.NullInt64
	if m.MonitorID != 0 {
		monitorID = sql.NullInt64{Int64: m.MonitorID, Valid: true}
	}
	res, err := s.write.ExecContext(ctx,
		`INSERT INTO maintenance_windows (name, starts_at, ends_at, namespace, monitor_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		m.Name, m.StartsAt.UnixMilli(), m.EndsAt.UnixMilli(), m.Namespace, monitorID, m.CreatedAt.UnixMilli())
	if err != nil {
		return Maintenance{}, fmt.Errorf("create maintenance: %w", err)
	}
	if m.ID, err = res.LastInsertId(); err != nil {
		return Maintenance{}, fmt.Errorf("create maintenance: %w", err)
	}
	return m, nil
}

func (s *SQLite) ListMaintenance(ctx context.Context, limit int) ([]Maintenance, error) {
	rows, err := s.read.QueryContext(ctx,
		`SELECT w.id, w.name, w.starts_at, w.ends_at, w.namespace, w.monitor_id, m.name, w.created_at
		 FROM maintenance_windows w LEFT JOIN monitors m ON m.id = w.monitor_id
		 ORDER BY w.starts_at DESC, w.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list maintenance: %w", err)
	}
	defer rows.Close()

	var windows []Maintenance
	for rows.Next() {
		var (
			m                           Maintenance
			startsAt, endsAt, createdAt int64
			monitorID                   sql.NullInt64
			monitorName                 sql.NullString
		)
		if err := rows.Scan(&m.ID, &m.Name, &startsAt, &endsAt, &m.Namespace, &monitorID, &monitorName, &createdAt); err != nil {
			return nil, fmt.Errorf("list maintenance: %w", err)
		}
		m.StartsAt = time.UnixMilli(startsAt).UTC()
		m.EndsAt = time.UnixMilli(endsAt).UTC()
		m.CreatedAt = time.UnixMilli(createdAt).UTC()
		m.MonitorID = monitorID.Int64
		m.MonitorName = monitorName.String
		windows = append(windows, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list maintenance: %w", err)
	}
	return windows, nil
}

func (s *SQLite) DeleteMaintenance(ctx context.Context, id int64) error {
	res, err := s.write.ExecContext(ctx, "DELETE FROM maintenance_windows WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete maintenance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete maintenance: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) InMaintenance(ctx context.Context, m Monitor, at time.Time) (bool, error) {
	var covered bool
	err := s.read.QueryRowContext(ctx,
		`SELECT EXISTS (
		     SELECT 1 FROM maintenance_windows w
		     WHERE w.starts_at <= ?1 AND w.ends_at > ?1
		       AND (w.monitor_id = ?2 OR (w.monitor_id IS NULL AND (w.namespace = '' OR w.namespace = ?3))))`,
		at.UnixMilli(), m.ID, m.Namespace).Scan(&covered)
	if err != nil {
		return false, fmt.Errorf("in maintenance: %w", err)
	}
	return covered, nil
}
