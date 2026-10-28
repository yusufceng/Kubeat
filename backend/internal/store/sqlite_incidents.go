package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// likeEscaper escapes the characters LIKE gives a meaning, with the escape
// character the queries name ("\"), so that a search for "50%" or "a_b"
// finds those very characters.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// incidentWhere is the FROM and WHERE part of what f selects, with its
// arguments. ok is false when f selects nothing at all, which an empty
// list of namespaces does: there is no need to ask the database then.
func incidentWhere(f IncidentFilter) (clause string, args []any, ok bool) {
	var conds []string
	if f.Namespaces != nil {
		if len(f.Namespaces) == 0 {
			return "", nil, false
		}
		conds = append(conds, "m.namespace IN (?"+strings.Repeat(", ?", len(f.Namespaces)-1)+")")
		for _, ns := range f.Namespaces {
			args = append(args, ns)
		}
	}
	if f.MonitorID != 0 {
		conds = append(conds, "i.monitor_id = ?")
		args = append(args, f.MonitorID)
	}
	if f.Kind != "" {
		conds = append(conds, "i.kind = ?")
		args = append(args, f.Kind)
	}
	if f.Open != nil {
		if *f.Open {
			conds = append(conds, "i.resolved_at IS NULL")
		} else {
			conds = append(conds, "i.resolved_at IS NOT NULL")
		}
	}
	if f.MonitorName != "" {
		conds = append(conds, `m.name LIKE ? ESCAPE '\'`)
		args = append(args, "%"+likeEscaper.Replace(f.MonitorName)+"%")
	}
	if !f.From.IsZero() {
		conds = append(conds, "(i.resolved_at IS NULL OR i.resolved_at > ?)")
		args = append(args, f.From.UnixMilli())
	}
	if !f.To.IsZero() {
		conds = append(conds, "i.started_at < ?")
		args = append(args, f.To.UnixMilli())
	}
	clause = " FROM incidents i JOIN monitors m ON m.id = i.monitor_id"
	if len(conds) > 0 {
		clause += " WHERE " + strings.Join(conds, " AND ")
	}
	return clause, args, true
}

// incidentOrder is the ORDER BY of f, with its arguments. Whatever the
// order, equal values are ordered by start, newest first, and then by id,
// so that the same filter pages through the same sequence.
func incidentOrder(f IncidentFilter) (string, []any) {
	dir := "ASC"
	if f.Desc {
		dir = "DESC"
	}
	switch f.Sort {
	case SortByDuration:
		now := f.Now
		if now.IsZero() {
			now = time.Now()
		}
		return " ORDER BY COALESCE(i.resolved_at, ?) - i.started_at " + dir + ", i.started_at DESC, i.id DESC", []any{now.UnixMilli()}
	case SortByMonitor:
		return " ORDER BY m.name COLLATE NOCASE " + dir + ", i.started_at DESC, i.id DESC", nil
	default:
		return " ORDER BY i.started_at " + dir + ", i.id " + dir, nil
	}
}

func (s *SQLite) FilterIncidents(ctx context.Context, f IncidentFilter) ([]Incident, error) {
	where, args, ok := incidentWhere(f)
	if !ok {
		return nil, nil
	}
	order, orderArgs := incidentOrder(f)
	query := "SELECT " + incidentColumns + where + order
	args = append(args, orderArgs...)
	switch {
	case f.Limit > 0:
		query += " LIMIT ? OFFSET ?"
		args = append(args, f.Limit, max(f.Offset, 0))
	case f.Offset > 0:
		query += " LIMIT -1 OFFSET ?"
		args = append(args, f.Offset)
	}
	rows, err := s.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("filter incidents: %w", err)
	}
	defer rows.Close()
	var incidents []Incident
	for rows.Next() {
		i, err := scanIncident(rows)
		if err != nil {
			return nil, fmt.Errorf("filter incidents: %w", err)
		}
		incidents = append(incidents, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("filter incidents: %w", err)
	}
	return incidents, nil
}

func (s *SQLite) CountIncidents(ctx context.Context, f IncidentFilter) (int, error) {
	where, args, ok := incidentWhere(f)
	if !ok {
		return 0, nil
	}
	var n int
	if err := s.read.QueryRowContext(ctx, "SELECT COUNT(*)"+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count incidents: %w", err)
	}
	return n, nil
}

// RecordDeliveries takes the monitor from the incident row itself, which
// is also how an attempt about a deleted incident is left out: there is no
// row to take it from.
func (s *SQLite) RecordDeliveries(ctx context.Context, deliveries []Delivery) error {
	if len(deliveries) == 0 {
		return nil
	}
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("record deliveries: %w", err)
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO incident_deliveries (incident_id, monitor_id, channel_id, channel_name, channel_type, event, event_at, group_size, at, delivered, error)
		 SELECT i.id, i.monitor_id, ?, ?, ?, ?, ?, ?, ?, ?, ? FROM incidents i WHERE i.id = ?`)
	if err != nil {
		return fmt.Errorf("record deliveries: %w", err)
	}
	defer stmt.Close()
	for _, d := range deliveries {
		_, err := stmt.ExecContext(ctx, d.ChannelID, d.ChannelName, d.ChannelType, d.Event, d.EventAt.UnixMilli(), max(d.GroupSize, 1),
			d.At.UnixMilli(), d.Delivered, d.Error, d.IncidentID)
		if err != nil {
			return fmt.Errorf("record deliveries: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record deliveries: %w", err)
	}
	return nil
}

const deliveryColumns = "id, incident_id, monitor_id, channel_id, channel_name, channel_type, event, event_at, group_size, at, delivered, error"

// scanDeliveries reads and closes rows of deliveryColumns.
func scanDeliveries(rows *sql.Rows) ([]Delivery, error) {
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		var (
			d           Delivery
			eventAt, at int64
		)
		if err := rows.Scan(&d.ID, &d.IncidentID, &d.MonitorID, &d.ChannelID, &d.ChannelName, &d.ChannelType, &d.Event, &eventAt,
			&d.GroupSize, &at, &d.Delivered, &d.Error); err != nil {
			return nil, err
		}
		d.EventAt, d.At = time.UnixMilli(eventAt).UTC(), time.UnixMilli(at).UTC()
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeliveriesEnds reads like FailedChecksEnds: the number of entries, the
// oldest, and when they do not all fit, the newest.
func (s *SQLite) DeliveriesEnds(ctx context.Context, incidentID int64, keep int) ([]Delivery, int, error) {
	var total int
	if err := s.read.QueryRowContext(ctx, "SELECT COUNT(*) FROM incident_deliveries WHERE incident_id = ?", incidentID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("deliveries: %w", err)
	}
	oldest, newest := Ends(total, keep)
	if oldest == 0 {
		return nil, total, nil
	}
	rows, err := s.read.QueryContext(ctx,
		"SELECT "+deliveryColumns+" FROM incident_deliveries WHERE incident_id = ? ORDER BY at, id LIMIT ?", incidentID, oldest)
	if err != nil {
		return nil, 0, fmt.Errorf("deliveries: %w", err)
	}
	first, err := scanDeliveries(rows)
	if err != nil {
		return nil, 0, fmt.Errorf("deliveries: %w", err)
	}
	if newest == 0 {
		return first, total, nil
	}
	rows, err = s.read.QueryContext(ctx,
		"SELECT "+deliveryColumns+" FROM (SELECT "+deliveryColumns+` FROM incident_deliveries WHERE incident_id = ?
		     ORDER BY at DESC, id DESC LIMIT ?) ORDER BY at, id`, incidentID, newest)
	if err != nil {
		return nil, 0, fmt.Errorf("deliveries: %w", err)
	}
	last, err := scanDeliveries(rows)
	if err != nil {
		return nil, 0, fmt.Errorf("deliveries: %w", err)
	}
	return append(first, last...), total, nil
}

// DeliveryAt looks for any attempt, delivered or not, and returns the
// first: the alert engine counts a channel as told of an outage once it
// tried to tell it, and runs the channel's reminders from then.
func (s *SQLite) DeliveryAt(ctx context.Context, incidentID, channelID int64, event string) (time.Time, bool, error) {
	var at sql.NullInt64
	err := s.read.QueryRowContext(ctx,
		"SELECT MIN(at) FROM incident_deliveries WHERE incident_id = ? AND channel_id = ? AND event = ?",
		incidentID, channelID, event).Scan(&at)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("delivery at: %w", err)
	}
	if !at.Valid {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(at.Int64).UTC(), true, nil
}

// Delivered says whether any attempt to tell the channel the event about
// the incident succeeded: the alert engine sends an outage notification a
// channel did not take again, and after a restart reads from here whether
// it still has to.
func (s *SQLite) Delivered(ctx context.Context, incidentID, channelID int64, event string) (bool, error) {
	var n int
	err := s.read.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM incident_deliveries WHERE incident_id = ? AND channel_id = ? AND event = ? AND delivered",
		incidentID, channelID, event).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("delivered: %w", err)
	}
	return n > 0, nil
}

func (s *SQLite) DeleteDeliveriesBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	n, err := s.deleteBefore(ctx, "incident_deliveries", "at", cutoff)
	if err != nil {
		return n, fmt.Errorf("delete deliveries: %w", err)
	}
	return n, nil
}

const noteColumns = "id, incident_id, author_id, author, text, show_on_status_page, created_at, updated_at"

func scanNote(row rowScanner) (IncidentNote, error) {
	var (
		n                IncidentNote
		authorID         sql.NullInt64
		created, updated int64
	)
	if err := row.Scan(&n.ID, &n.IncidentID, &authorID, &n.Author, &n.Text, &n.ShowOnStatusPage, &created, &updated); err != nil {
		return IncidentNote{}, err
	}
	n.AuthorID = authorID.Int64
	n.CreatedAt, n.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	return n, nil
}

// CreateIncidentNote refers to the author's account only while it exists:
// the API token has none, and an account deleted since the request was
// authenticated has none any more.
func (s *SQLite) CreateIncidentNote(ctx context.Context, n IncidentNote) (IncidentNote, error) {
	n.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
	n.UpdatedAt = n.CreatedAt
	res, err := s.write.ExecContext(ctx,
		`INSERT INTO incident_notes (incident_id, author_id, author, text, show_on_status_page, created_at, updated_at)
		 SELECT i.id, (SELECT u.id FROM users u WHERE u.id = ?), ?, ?, ?, ?, ? FROM incidents i WHERE i.id = ?`,
		n.AuthorID, n.Author, n.Text, n.ShowOnStatusPage, n.CreatedAt.UnixMilli(), n.UpdatedAt.UnixMilli(), n.IncidentID)
	if err != nil {
		return IncidentNote{}, fmt.Errorf("create incident note: %w", err)
	}
	if rows, err := res.RowsAffected(); err != nil {
		return IncidentNote{}, fmt.Errorf("create incident note: %w", err)
	} else if rows == 0 {
		return IncidentNote{}, ErrNotFound // the incident is gone
	}
	if n.ID, err = res.LastInsertId(); err != nil {
		return IncidentNote{}, fmt.Errorf("create incident note: %w", err)
	}
	// Read back through the writer: the author may have had no account.
	created, err := scanNote(s.write.QueryRowContext(ctx, "SELECT "+noteColumns+" FROM incident_notes WHERE id = ?", n.ID))
	if err != nil {
		return IncidentNote{}, fmt.Errorf("create incident note: %w", err)
	}
	return created, nil
}

func (s *SQLite) GetIncidentNote(ctx context.Context, id int64) (IncidentNote, error) {
	n, err := scanNote(s.read.QueryRowContext(ctx, "SELECT "+noteColumns+" FROM incident_notes WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return IncidentNote{}, ErrNotFound
	}
	if err != nil {
		return IncidentNote{}, fmt.Errorf("get incident note: %w", err)
	}
	return n, nil
}

func (s *SQLite) ListIncidentNotes(ctx context.Context, incidentID int64, limit int) ([]IncidentNote, error) {
	rows, err := s.read.QueryContext(ctx,
		"SELECT "+noteColumns+" FROM incident_notes WHERE incident_id = ? ORDER BY created_at, id LIMIT ?", incidentID, limit)
	if err != nil {
		return nil, fmt.Errorf("list incident notes: %w", err)
	}
	defer rows.Close()
	var notes []IncidentNote
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, fmt.Errorf("list incident notes: %w", err)
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list incident notes: %w", err)
	}
	return notes, nil
}

func (s *SQLite) UpdateIncidentNote(ctx context.Context, n IncidentNote) (IncidentNote, error) {
	res, err := s.write.ExecContext(ctx,
		"UPDATE incident_notes SET text = ?, show_on_status_page = ?, updated_at = ? WHERE id = ?",
		n.Text, n.ShowOnStatusPage, time.Now().UTC().UnixMilli(), n.ID)
	if err != nil {
		return IncidentNote{}, fmt.Errorf("update incident note: %w", err)
	}
	if rows, err := res.RowsAffected(); err != nil {
		return IncidentNote{}, fmt.Errorf("update incident note: %w", err)
	} else if rows == 0 {
		return IncidentNote{}, ErrNotFound
	}
	// Read back through the writer so the answer reflects this update.
	updated, err := scanNote(s.write.QueryRowContext(ctx, "SELECT "+noteColumns+" FROM incident_notes WHERE id = ?", n.ID))
	if err != nil {
		return IncidentNote{}, fmt.Errorf("update incident note: %w", err)
	}
	return updated, nil
}

func (s *SQLite) DeleteIncidentNote(ctx context.Context, id int64) error {
	res, err := s.write.ExecContext(ctx, "DELETE FROM incident_notes WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete incident note: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete incident note: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
