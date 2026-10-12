package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const incidentColumns = "i.id, i.monitor_id, i.kind, m.name, i.started_at, i.resolved_at, i.cause, i.last_notified_at"

func scanIncident(row rowScanner) (Incident, error) {
	var (
		i            Incident
		startedAt    int64
		resolvedAt   sql.NullInt64
		lastNotified sql.NullInt64
	)
	if err := row.Scan(&i.ID, &i.MonitorID, &i.Kind, &i.MonitorName, &startedAt, &resolvedAt, &i.Cause, &lastNotified); err != nil {
		return Incident{}, err
	}
	i.StartedAt = time.UnixMilli(startedAt).UTC()
	i.LastNotifiedAt = unixMilliOrZero(lastNotified)
	if resolvedAt.Valid {
		i.ResolvedAt = time.UnixMilli(resolvedAt.Int64).UTC()
	}
	return i, nil
}

func (s *SQLite) OpenIncident(ctx context.Context, i Incident) (Incident, error) {
	if i.Kind == "" {
		i.Kind = IncidentDown
	}
	var lastNotified sql.NullInt64
	if !i.LastNotifiedAt.IsZero() {
		i.LastNotifiedAt = i.LastNotifiedAt.UTC().Truncate(time.Millisecond)
		lastNotified = sql.NullInt64{Int64: i.LastNotifiedAt.UnixMilli(), Valid: true}
	}
	res, err := s.write.ExecContext(ctx,
		"INSERT INTO incidents (monitor_id, kind, started_at, cause, last_notified_at) VALUES (?, ?, ?, ?, ?)",
		i.MonitorID, i.Kind, i.StartedAt.UnixMilli(), i.Cause, lastNotified)
	if isUniqueViolation(err) {
		return Incident{}, ErrConflict
	}
	if err != nil {
		return Incident{}, fmt.Errorf("open incident: %w", err)
	}
	if i.ID, err = res.LastInsertId(); err != nil {
		return Incident{}, fmt.Errorf("open incident: %w", err)
	}
	i.StartedAt = i.StartedAt.UTC().Truncate(time.Millisecond)
	i.ResolvedAt = time.Time{}
	return i, nil
}

func (s *SQLite) ResolveIncident(ctx context.Context, id int64, resolvedAt time.Time) error {
	res, err := s.write.ExecContext(ctx,
		"UPDATE incidents SET resolved_at = ? WHERE id = ? AND resolved_at IS NULL", resolvedAt.UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("resolve incident: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("resolve incident: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) MarkIncidentNotified(ctx context.Context, id int64, at time.Time) error {
	res, err := s.write.ExecContext(ctx, "UPDATE incidents SET last_notified_at = ? WHERE id = ?", at.UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("mark incident notified: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark incident notified: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) GetOpenIncident(ctx context.Context, monitorID int64, kind IncidentKind) (Incident, error) {
	i, err := scanIncident(s.read.QueryRowContext(ctx,
		"SELECT "+incidentColumns+` FROM incidents i JOIN monitors m ON m.id = i.monitor_id
		 WHERE i.monitor_id = ? AND i.kind = ? AND i.resolved_at IS NULL`, monitorID, kind))
	if errors.Is(err, sql.ErrNoRows) {
		return Incident{}, ErrNotFound
	}
	if err != nil {
		return Incident{}, fmt.Errorf("get open incident: %w", err)
	}
	return i, nil
}

func (s *SQLite) ListIncidents(ctx context.Context, monitorID int64, limit int) ([]Incident, error) {
	rows, err := s.read.QueryContext(ctx,
		"SELECT "+incidentColumns+` FROM incidents i JOIN monitors m ON m.id = i.monitor_id
		 WHERE ?1 = 0 OR i.monitor_id = ?1
		 ORDER BY i.started_at DESC, i.id DESC LIMIT ?2`, monitorID, limit)
	if err != nil {
		return nil, fmt.Errorf("list incidents: %w", err)
	}
	defer rows.Close()

	var incidents []Incident
	for rows.Next() {
		i, err := scanIncident(rows)
		if err != nil {
			return nil, fmt.Errorf("list incidents: %w", err)
		}
		incidents = append(incidents, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list incidents: %w", err)
	}
	return incidents, nil
}

const channelColumns = "id, name, type, target, enabled, created_at"

func scanChannel(row rowScanner) (Channel, error) {
	var (
		c         Channel
		createdAt int64
	)
	if err := row.Scan(&c.ID, &c.Name, &c.Type, &c.Target, &c.Enabled, &createdAt); err != nil {
		return Channel{}, err
	}
	c.CreatedAt = time.UnixMilli(createdAt).UTC()
	return c, nil
}

func (s *SQLite) CreateChannel(ctx context.Context, c Channel) (Channel, error) {
	c.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
	res, err := s.write.ExecContext(ctx,
		"INSERT INTO notification_channels (name, type, target, enabled, created_at) VALUES (?, ?, ?, ?, ?)",
		c.Name, c.Type, c.Target, c.Enabled, c.CreatedAt.UnixMilli())
	if isUniqueViolation(err) {
		return Channel{}, ErrConflict
	}
	if err != nil {
		return Channel{}, fmt.Errorf("create channel: %w", err)
	}
	if c.ID, err = res.LastInsertId(); err != nil {
		return Channel{}, fmt.Errorf("create channel: %w", err)
	}
	return c, nil
}

func (s *SQLite) GetChannel(ctx context.Context, id int64) (Channel, error) {
	c, err := scanChannel(s.read.QueryRowContext(ctx,
		"SELECT "+channelColumns+" FROM notification_channels WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Channel{}, ErrNotFound
	}
	if err != nil {
		return Channel{}, fmt.Errorf("get channel: %w", err)
	}
	return c, nil
}

func (s *SQLite) ListChannels(ctx context.Context) ([]Channel, error) {
	rows, err := s.read.QueryContext(ctx, "SELECT "+channelColumns+" FROM notification_channels ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("list channels: %w", err)
		}
		channels = append(channels, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	return channels, nil
}

func (s *SQLite) DeleteChannel(ctx context.Context, id int64) error {
	res, err := s.write.ExecContext(ctx, "DELETE FROM notification_channels WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete channel: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete channel: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
