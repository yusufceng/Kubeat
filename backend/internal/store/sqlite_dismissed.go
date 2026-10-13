package store

import (
	"context"
	"fmt"
	"time"
)

// DismissedSource is an annotated cluster object whose monitor was deleted
// in Kubeat: the annotation still asks for it, the deletion says no.
type DismissedSource struct {
	Namespace, Source string
	At                time.Time
	By                string
}

// DismissSource records that the monitor of a cluster object was deleted
// by hand, so that discovery does not create it again.
func (s *SQLite) DismissSource(ctx context.Context, d DismissedSource) error {
	_, err := s.write.ExecContext(ctx,
		`INSERT INTO dismissed_sources (namespace, source, dismissed_at, dismissed_by) VALUES (?, ?, ?, ?)
		 ON CONFLICT (namespace, source) DO UPDATE SET dismissed_at = excluded.dismissed_at, dismissed_by = excluded.dismissed_by`,
		d.Namespace, d.Source, d.At.UnixMilli(), d.By)
	if err != nil {
		return fmt.Errorf("dismiss source: %w", err)
	}
	return nil
}

// RestoreSource forgets a dismissal; ErrNotFound when there was none.
func (s *SQLite) RestoreSource(ctx context.Context, namespace, source string) error {
	res, err := s.write.ExecContext(ctx, "DELETE FROM dismissed_sources WHERE namespace = ? AND source = ?", namespace, source)
	if err != nil {
		return fmt.Errorf("restore source: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListDismissedSources returns every dismissal.
func (s *SQLite) ListDismissedSources(ctx context.Context) ([]DismissedSource, error) {
	rows, err := s.read.QueryContext(ctx, "SELECT namespace, source, dismissed_at, dismissed_by FROM dismissed_sources ORDER BY namespace, source")
	if err != nil {
		return nil, fmt.Errorf("list dismissed sources: %w", err)
	}
	defer rows.Close()
	var out []DismissedSource
	for rows.Next() {
		var (
			d  DismissedSource
			at int64
		)
		if err := rows.Scan(&d.Namespace, &d.Source, &at, &d.By); err != nil {
			return nil, fmt.Errorf("list dismissed sources: %w", err)
		}
		d.At = time.UnixMilli(at).UTC()
		out = append(out, d)
	}
	return out, rows.Err()
}
