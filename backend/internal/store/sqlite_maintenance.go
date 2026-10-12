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
		`INSERT INTO maintenance_windows (name, starts_at, ends_at, repeat, timezone, namespace, monitor_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Name, m.StartsAt.UnixMilli(), m.EndsAt.UnixMilli(), m.Repeat, m.Timezone, m.Namespace, monitorID, m.CreatedAt.UnixMilli())
	if err != nil {
		return Maintenance{}, fmt.Errorf("create maintenance: %w", err)
	}
	if m.ID, err = res.LastInsertId(); err != nil {
		return Maintenance{}, fmt.Errorf("create maintenance: %w", err)
	}
	return m, nil
}

const maintenanceQuery = `SELECT w.id, w.name, w.starts_at, w.ends_at, w.repeat, w.timezone, w.namespace, w.monitor_id, m.name, m.namespace, w.created_at
		 FROM maintenance_windows w LEFT JOIN monitors m ON m.id = w.monitor_id `

// scanMaintenance reads and closes rows of maintenanceQuery.
func scanMaintenance(rows *sql.Rows) ([]Maintenance, error) {
	defer rows.Close()
	var windows []Maintenance
	for rows.Next() {
		var (
			m                           Maintenance
			startsAt, endsAt, createdAt int64
			monitorID                   sql.NullInt64
			monitorName, monitorNS      sql.NullString
		)
		if err := rows.Scan(&m.ID, &m.Name, &startsAt, &endsAt, &m.Repeat, &m.Timezone, &m.Namespace, &monitorID, &monitorName, &monitorNS, &createdAt); err != nil {
			return nil, err
		}
		m.MonitorNamespace = monitorNS.String
		m.StartsAt = time.UnixMilli(startsAt).UTC()
		m.EndsAt = time.UnixMilli(endsAt).UTC()
		m.CreatedAt = time.UnixMilli(createdAt).UTC()
		m.MonitorID = monitorID.Int64
		m.MonitorName = monitorName.String
		windows = append(windows, m)
	}
	return windows, rows.Err()
}

func (s *SQLite) ListMaintenance(ctx context.Context, limit int) ([]Maintenance, error) {
	rows, err := s.read.QueryContext(ctx,
		maintenanceQuery+`ORDER BY w.repeat != '' DESC, w.starts_at DESC, w.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list maintenance: %w", err)
	}
	windows, err := scanMaintenance(rows)
	if err != nil {
		return nil, fmt.Errorf("list maintenance: %w", err)
	}
	return windows, nil
}

// activeMaintenance returns the windows that cover at, each with the end
// of the occurrence that does. When a repeating window is due is worked
// out here, not in SQL: it depends on the calendar of a time zone.
func (s *SQLite) activeMaintenance(ctx context.Context, at time.Time) ([]Maintenance, []time.Time, error) {
	rows, err := s.read.QueryContext(ctx,
		maintenanceQuery+`WHERE w.repeat != '' OR (w.starts_at <= ?1 AND w.ends_at > ?1)`, at.UnixMilli())
	if err != nil {
		return nil, nil, err
	}
	candidates, err := scanMaintenance(rows)
	if err != nil {
		return nil, nil, err
	}
	var (
		active []Maintenance
		until  []time.Time
	)
	for _, w := range candidates {
		if end, ok := w.ActiveUntil(at); ok {
			active = append(active, w)
			until = append(until, end)
		}
	}
	return active, until, nil
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
	active, _, err := s.activeMaintenance(ctx, at)
	if err != nil {
		return false, fmt.Errorf("in maintenance: %w", err)
	}
	for _, w := range active {
		if w.Covers(m) {
			return true, nil
		}
	}
	return false, nil
}
