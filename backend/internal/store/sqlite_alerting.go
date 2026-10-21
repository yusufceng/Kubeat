package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

const incidentColumns = "i.id, i.monitor_id, i.kind, m.name, m.namespace, i.started_at, i.resolved_at, i.cause, i.last_notified_at, i.opened_at"

func scanIncident(row rowScanner) (Incident, error) {
	var (
		i                    Incident
		startedAt            int64
		resolvedAt           sql.NullInt64
		lastNotified, opened sql.NullInt64
	)
	if err := row.Scan(&i.ID, &i.MonitorID, &i.Kind, &i.MonitorName, &i.MonitorNamespace, &startedAt, &resolvedAt, &i.Cause, &lastNotified, &opened); err != nil {
		return Incident{}, err
	}
	i.StartedAt = time.UnixMilli(startedAt).UTC()
	i.LastNotifiedAt = unixMilliOrZero(lastNotified)
	i.OpenedAt = unixMilliOrZero(opened)
	if resolvedAt.Valid {
		i.ResolvedAt = time.UnixMilli(resolvedAt.Int64).UTC()
	}
	return i, nil
}

// nullMilli is the stored form of a time that may be unknown: NULL for the
// zero time, Unix milliseconds otherwise. It also returns the time as it
// reads back.
func nullMilli(t time.Time) (sql.NullInt64, time.Time) {
	if t.IsZero() {
		return sql.NullInt64{}, time.Time{}
	}
	t = t.UTC().Truncate(time.Millisecond)
	return sql.NullInt64{Int64: t.UnixMilli(), Valid: true}, t
}

func (s *SQLite) OpenIncident(ctx context.Context, i Incident) (Incident, error) {
	if i.Kind == "" {
		i.Kind = IncidentDown
	}
	var lastNotified, opened sql.NullInt64
	lastNotified, i.LastNotifiedAt = nullMilli(i.LastNotifiedAt)
	opened, i.OpenedAt = nullMilli(i.OpenedAt)
	res, err := s.write.ExecContext(ctx,
		"INSERT INTO incidents (monitor_id, kind, started_at, cause, last_notified_at, opened_at) VALUES (?, ?, ?, ?, ?, ?)",
		i.MonitorID, i.Kind, i.StartedAt.UnixMilli(), i.Cause, lastNotified, opened)
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

func (s *SQLite) GetIncident(ctx context.Context, id int64) (Incident, error) {
	i, err := scanIncident(s.read.QueryRowContext(ctx,
		"SELECT "+incidentColumns+" FROM incidents i JOIN monitors m ON m.id = i.monitor_id WHERE i.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Incident{}, ErrNotFound
	}
	if err != nil {
		return Incident{}, fmt.Errorf("get incident: %w", err)
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

const channelColumns = "id, name, type, target, secret, namespaces, enabled, created_at, tags, events, last_attempt_at, last_ok, last_error, delay_seconds, repeat_seconds"

// scanChannel reads a channel and decrypts its target and secret.
func (s *SQLite) scanChannel(row rowScanner) (Channel, error) {
	c, err := scanRawChannel(row)
	if err != nil {
		return Channel{}, err
	}
	if c.Target, err = s.secrets.Open(c.Target); err == nil {
		c.Secret, err = s.secrets.Open(c.Secret)
	}
	if err != nil {
		return Channel{}, fmt.Errorf("channel %d: %w", c.ID, err)
	}
	return c, nil
}

// sealChannel returns the stored forms of a channel's target and secret.
// The target is encrypted too: a webhook address lets its holder post.
func (s *SQLite) sealChannel(c Channel) (target, secret string, err error) {
	if target, err = s.secrets.Seal(c.Target); err != nil {
		return "", "", err
	}
	secret, err = s.secrets.Seal(c.Secret)
	return target, secret, err
}

func scanRawChannel(row rowScanner) (Channel, error) {
	var (
		c                        Channel
		namespaces, tags, events string
		createdAt                int64
		lastAttempt              sql.NullInt64
		lastOK                   bool
		lastError                string
		delay, repeat            int64
		err                      error
	)
	if err := row.Scan(&c.ID, &c.Name, &c.Type, &c.Target, &c.Secret, &namespaces, &c.Enabled, &createdAt, &tags, &events, &lastAttempt, &lastOK, &lastError, &delay, &repeat); err != nil {
		return Channel{}, err
	}
	c.Delay = time.Duration(delay) * time.Second
	c.Repeat = time.Duration(repeat) * time.Second
	// The delivery columns mean nothing until something was attempted.
	if lastAttempt.Valid {
		c.LastAttemptAt = time.UnixMilli(lastAttempt.Int64).UTC()
		c.LastOK, c.LastError = lastOK, lastError
	}
	if c.Namespaces, err = decodeNamespaces(namespaces); err != nil {
		return Channel{}, fmt.Errorf("channel %d: %w", c.ID, err)
	}
	if c.Tags, err = decodeNamespaces(tags); err != nil {
		return Channel{}, fmt.Errorf("channel %d: %w", c.ID, err)
	}
	if c.Events, err = decodeNamespaces(events); err != nil {
		return Channel{}, fmt.Errorf("channel %d: %w", c.ID, err)
	}
	c.CreatedAt = time.UnixMilli(createdAt).UTC()
	return c, nil
}

// encodeNamespaces stores a list of namespaces as a JSON array; none is "[]".
func encodeNamespaces(namespaces []string) (string, error) {
	if len(namespaces) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(namespaces)
	return string(b), err
}

// decodeNamespaces reads what encodeNamespaces wrote; "[]" is nil.
func decodeNamespaces(text string) ([]string, error) {
	var namespaces []string
	if err := json.Unmarshal([]byte(text), &namespaces); err != nil {
		return nil, fmt.Errorf("decode namespaces: %w", err)
	}
	if len(namespaces) == 0 {
		return nil, nil
	}
	return namespaces, nil
}

func (s *SQLite) CreateChannel(ctx context.Context, c Channel) (Channel, error) {
	if !slices.Contains(ChannelTypes, c.Type) {
		return Channel{}, fmt.Errorf("create channel: unknown type %q", c.Type)
	}
	namespaces, tags, events, err := encodeChannelLists(c)
	if err != nil {
		return Channel{}, fmt.Errorf("create channel: %w", err)
	}
	if len(c.Namespaces) == 0 {
		c.Namespaces = nil
	}
	if len(c.Tags) == 0 {
		c.Tags = nil
	}
	if len(c.Events) == 0 {
		c.Events = nil
	}
	target, secret, err := s.sealChannel(c)
	if err != nil {
		return Channel{}, fmt.Errorf("create channel: %w", err)
	}
	c.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
	c.Delay, c.Repeat = c.Delay.Truncate(time.Second), c.Repeat.Truncate(time.Second)
	res, err := s.write.ExecContext(ctx,
		"INSERT INTO notification_channels (name, type, target, secret, namespaces, enabled, created_at, tags, events, delay_seconds, repeat_seconds) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		c.Name, c.Type, target, secret, namespaces, c.Enabled, c.CreatedAt.UnixMilli(), tags, events, int64(c.Delay/time.Second), int64(c.Repeat/time.Second))
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

// encodeChannelLists is the stored form of a channel's scope lists.
func encodeChannelLists(c Channel) (namespaces, tags, events string, err error) {
	if namespaces, err = encodeNamespaces(c.Namespaces); err != nil {
		return "", "", "", err
	}
	if tags, err = encodeNamespaces(c.Tags); err != nil {
		return "", "", "", err
	}
	events, err = encodeNamespaces(c.Events)
	return namespaces, tags, events, err
}

func (s *SQLite) UpdateChannel(ctx context.Context, c Channel) (Channel, error) {
	namespaces, tags, events, err := encodeChannelLists(c)
	if err != nil {
		return Channel{}, fmt.Errorf("update channel: %w", err)
	}
	target, secret, err := s.sealChannel(c)
	if err != nil {
		return Channel{}, fmt.Errorf("update channel: %w", err)
	}
	res, err := s.write.ExecContext(ctx,
		"UPDATE notification_channels SET name = ?, target = ?, secret = ?, namespaces = ?, enabled = ?, tags = ?, events = ?, delay_seconds = ?, repeat_seconds = ? WHERE id = ?",
		c.Name, target, secret, namespaces, c.Enabled, tags, events, int64(c.Delay/time.Second), int64(c.Repeat/time.Second), c.ID)
	if isUniqueViolation(err) {
		return Channel{}, ErrConflict
	}
	if err != nil {
		return Channel{}, fmt.Errorf("update channel: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Channel{}, fmt.Errorf("update channel: %w", err)
	}
	if n == 0 {
		return Channel{}, ErrNotFound
	}
	// Read back through the writer so the answer reflects this update.
	updated, err := s.scanChannel(s.write.QueryRowContext(ctx,
		"SELECT "+channelColumns+" FROM notification_channels WHERE id = ?", c.ID))
	if err != nil {
		return Channel{}, fmt.Errorf("update channel: %w", err)
	}
	return updated, nil
}

// RecordChannelDelivery notes the outcome of a delivery attempt. A channel
// deleted in the meantime is not an error: the attempt still happened.
func (s *SQLite) RecordChannelDelivery(ctx context.Context, id int64, at time.Time, deliveryError string) error {
	_, err := s.write.ExecContext(ctx,
		"UPDATE notification_channels SET last_attempt_at = ?, last_ok = ?, last_error = ? WHERE id = ?",
		at.UnixMilli(), deliveryError == "", deliveryError, id)
	if err != nil {
		return fmt.Errorf("record channel delivery: %w", err)
	}
	return nil
}

func (s *SQLite) GetChannel(ctx context.Context, id int64) (Channel, error) {
	c, err := s.scanChannel(s.read.QueryRowContext(ctx,
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
		c, err := s.scanChannel(rows)
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
