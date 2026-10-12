package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const statusPageColumns = "id, slug, title, description, announcement, published, created_at"

func scanStatusPage(row rowScanner) (StatusPage, error) {
	var (
		p         StatusPage
		createdAt int64
	)
	if err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Description, &p.Announcement, &p.Published, &createdAt); err != nil {
		return StatusPage{}, err
	}
	p.CreatedAt = time.UnixMilli(createdAt).UTC()
	return p, nil
}

// queryer is what reads a status page: either pool, or a transaction.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// statusPage reads one page with its monitors; where is "id = ?" or "slug = ?".
func statusPage(ctx context.Context, q queryer, where string, arg any) (StatusPage, error) {
	p, err := scanStatusPage(q.QueryRowContext(ctx, "SELECT "+statusPageColumns+" FROM status_pages WHERE "+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return StatusPage{}, ErrNotFound
	}
	if err != nil {
		return StatusPage{}, err
	}
	rows, err := q.QueryContext(ctx,
		"SELECT monitor_id, display_name FROM status_page_monitors WHERE page_id = ? ORDER BY position", p.ID)
	if err != nil {
		return StatusPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var m StatusPageMonitor
		if err := rows.Scan(&m.MonitorID, &m.DisplayName); err != nil {
			return StatusPage{}, err
		}
		p.Monitors = append(p.Monitors, m)
	}
	return p, rows.Err()
}

// setStatusPageMonitors replaces the monitors of a page, in the given order.
func setStatusPageMonitors(ctx context.Context, tx *sql.Tx, pageID int64, monitors []StatusPageMonitor) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM status_page_monitors WHERE page_id = ?", pageID); err != nil {
		return err
	}
	for position, m := range monitors {
		_, err := tx.ExecContext(ctx,
			"INSERT INTO status_page_monitors (page_id, monitor_id, position, display_name) VALUES (?, ?, ?, ?)",
			pageID, m.MonitorID, position, m.DisplayName)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLite) CreateStatusPage(ctx context.Context, p StatusPage) (StatusPage, error) {
	p.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return StatusPage{}, fmt.Errorf("create status page: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		"INSERT INTO status_pages (slug, title, description, announcement, published, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		p.Slug, p.Title, p.Description, p.Announcement, p.Published, p.CreatedAt.UnixMilli())
	if isUniqueViolation(err) {
		return StatusPage{}, ErrConflict
	}
	if err != nil {
		return StatusPage{}, fmt.Errorf("create status page: %w", err)
	}
	if p.ID, err = res.LastInsertId(); err != nil {
		return StatusPage{}, fmt.Errorf("create status page: %w", err)
	}
	if err := setStatusPageMonitors(ctx, tx, p.ID, p.Monitors); err != nil {
		return StatusPage{}, fmt.Errorf("create status page: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return StatusPage{}, fmt.Errorf("create status page: %w", err)
	}
	if len(p.Monitors) == 0 {
		p.Monitors = nil
	}
	return p, nil
}

func (s *SQLite) GetStatusPage(ctx context.Context, id int64) (StatusPage, error) {
	p, err := statusPage(ctx, s.read, "id = ?", id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return StatusPage{}, fmt.Errorf("get status page: %w", err)
	}
	return p, err
}

func (s *SQLite) GetStatusPageBySlug(ctx context.Context, slug string) (StatusPage, error) {
	p, err := statusPage(ctx, s.read, "slug = ?", slug)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return StatusPage{}, fmt.Errorf("get status page: %w", err)
	}
	return p, err
}

func (s *SQLite) ListStatusPages(ctx context.Context) ([]StatusPage, error) {
	rows, err := s.read.QueryContext(ctx, "SELECT id FROM status_pages ORDER BY title, id")
	if err != nil {
		return nil, fmt.Errorf("list status pages: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("list status pages: %w", err)
		}
		ids = append(ids, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, fmt.Errorf("list status pages: %w", err)
	}

	pages := make([]StatusPage, 0, len(ids))
	for _, id := range ids {
		p, err := statusPage(ctx, s.read, "id = ?", id)
		if errors.Is(err, ErrNotFound) {
			continue // deleted in between
		}
		if err != nil {
			return nil, fmt.Errorf("list status pages: %w", err)
		}
		pages = append(pages, p)
	}
	return pages, nil
}

func (s *SQLite) UpdateStatusPage(ctx context.Context, p StatusPage) (StatusPage, error) {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return StatusPage{}, fmt.Errorf("update status page: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		"UPDATE status_pages SET slug = ?, title = ?, description = ?, announcement = ?, published = ? WHERE id = ?",
		p.Slug, p.Title, p.Description, p.Announcement, p.Published, p.ID)
	if isUniqueViolation(err) {
		return StatusPage{}, ErrConflict
	}
	if err != nil {
		return StatusPage{}, fmt.Errorf("update status page: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return StatusPage{}, fmt.Errorf("update status page: %w", err)
	} else if n == 0 {
		return StatusPage{}, ErrNotFound
	}
	if err := setStatusPageMonitors(ctx, tx, p.ID, p.Monitors); err != nil {
		return StatusPage{}, fmt.Errorf("update status page: %w", err)
	}
	updated, err := statusPage(ctx, tx, "id = ?", p.ID)
	if err != nil {
		return StatusPage{}, fmt.Errorf("update status page: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return StatusPage{}, fmt.Errorf("update status page: %w", err)
	}
	return updated, nil
}

func (s *SQLite) DeleteStatusPage(ctx context.Context, id int64) error {
	res, err := s.write.ExecContext(ctx, "DELETE FROM status_pages WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete status page: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete status page: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) StatusPageDailyUptime(ctx context.Context, pageID int64, from, to time.Time) (map[int64][]SeriesPoint, error) {
	day := (24 * time.Hour).Milliseconds()
	rows, err := s.read.QueryContext(ctx,
		`SELECT r.monitor_id, (r.bucket_start / ?1) * ?1 AS day, SUM(r.checks), SUM(r.up_checks)
		 FROM check_rollups r
		 JOIN status_page_monitors p ON p.monitor_id = r.monitor_id AND p.page_id = ?2
		 WHERE r.bucket_start >= ?3 AND r.bucket_start < ?4
		 GROUP BY r.monitor_id, day ORDER BY r.monitor_id, day`,
		day, pageID, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("status page uptime: %w", err)
	}
	defer rows.Close()

	out := make(map[int64][]SeriesPoint)
	for rows.Next() {
		var (
			monitorID, start int64
			p                SeriesPoint
		)
		if err := rows.Scan(&monitorID, &start, &p.Checks, &p.UpChecks); err != nil {
			return nil, fmt.Errorf("status page uptime: %w", err)
		}
		p.Start = time.UnixMilli(start).UTC()
		out[monitorID] = append(out[monitorID], p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("status page uptime: %w", err)
	}
	return out, nil
}

func (s *SQLite) MonitorIsPublic(ctx context.Context, monitorID int64) (bool, error) {
	var public bool
	err := s.read.QueryRowContext(ctx,
		`SELECT EXISTS (
		     SELECT 1 FROM status_page_monitors m JOIN status_pages p ON p.id = m.page_id
		     WHERE m.monitor_id = ? AND p.published)`, monitorID).Scan(&public)
	if err != nil {
		return false, fmt.Errorf("monitor is public: %w", err)
	}
	return public, nil
}
