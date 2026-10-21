package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const statusPageColumns = "id, slug, title, description, announcement, published, noindex, hide_paused, language, detail_pages, password_hash, created_at"

func scanStatusPage(row rowScanner) (StatusPage, error) {
	var (
		p         StatusPage
		createdAt int64
	)
	if err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Description, &p.Announcement, &p.Published,
		&p.NoIndex, &p.HidePaused, &p.Language, &p.DetailPages, &p.PasswordHash, &createdAt); err != nil {
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

// statusPage reads one page with its sections, monitors and rules; where
// is "id = ?" or "slug = ?".
func statusPage(ctx context.Context, q queryer, where string, arg any) (StatusPage, error) {
	p, err := scanStatusPage(q.QueryRowContext(ctx, "SELECT "+statusPageColumns+" FROM status_pages WHERE "+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return StatusPage{}, ErrNotFound
	}
	if err != nil {
		return StatusPage{}, err
	}
	err = eachRow(ctx, q, "SELECT name FROM status_page_sections WHERE page_id = ? ORDER BY position", func(rows *sql.Rows) error {
		var s StatusPageSection
		if err := rows.Scan(&s.Name); err != nil {
			return err
		}
		p.Sections = append(p.Sections, s)
		return nil
	}, p.ID)
	if err != nil {
		return StatusPage{}, err
	}
	err = eachRow(ctx, q,
		`SELECT m.monitor_id, m.display_name, COALESCE(s.name, '') FROM status_page_monitors m
		 LEFT JOIN status_page_sections s ON s.id = m.section_id WHERE m.page_id = ? ORDER BY m.position`,
		func(rows *sql.Rows) error {
			var m StatusPageMonitor
			if err := rows.Scan(&m.MonitorID, &m.DisplayName, &m.Section); err != nil {
				return err
			}
			p.Monitors = append(p.Monitors, m)
			return nil
		}, p.ID)
	if err != nil {
		return StatusPage{}, err
	}
	err = eachRow(ctx, q,
		`SELECT r.namespace, r.tag, COALESCE(s.name, '') FROM status_page_rules r
		 LEFT JOIN status_page_sections s ON s.id = r.section_id WHERE r.page_id = ? ORDER BY r.position`,
		func(rows *sql.Rows) error {
			var r StatusPageRule
			if err := rows.Scan(&r.Namespace, &r.Tag, &r.Section); err != nil {
				return err
			}
			p.Rules = append(p.Rules, r)
			return nil
		}, p.ID)
	if err != nil {
		return StatusPage{}, err
	}
	return p, nil
}

// eachRow runs query and calls scan for each of its rows.
func eachRow(ctx context.Context, q queryer, query string, scan func(*sql.Rows) error, args ...any) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// setStatusPageContents replaces the sections, monitors and rules of a
// page, in the given orders. A monitor or rule that names a section the
// page does not have is an error, as is a section named twice.
func setStatusPageContents(ctx context.Context, tx *sql.Tx, pageID int64, p StatusPage) error {
	for _, table := range []string{"status_page_rules", "status_page_monitors", "status_page_sections"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE page_id = ?", pageID); err != nil {
			return err
		}
	}
	sections := make(map[string]int64, len(p.Sections))
	for position, s := range p.Sections {
		if s.Name == "" {
			return errors.New("a section needs a name")
		}
		if _, dup := sections[s.Name]; dup {
			return fmt.Errorf("section %q is listed twice", s.Name)
		}
		res, err := tx.ExecContext(ctx,
			"INSERT INTO status_page_sections (page_id, name, position) VALUES (?, ?, ?)", pageID, s.Name, position)
		if err != nil {
			return err
		}
		if sections[s.Name], err = res.LastInsertId(); err != nil {
			return err
		}
	}
	sectionOf := func(name string) (sql.NullInt64, error) {
		if name == "" {
			return sql.NullInt64{}, nil
		}
		id, ok := sections[name]
		if !ok {
			return sql.NullInt64{}, fmt.Errorf("the page has no section %q", name)
		}
		return sql.NullInt64{Int64: id, Valid: true}, nil
	}
	for position, m := range p.Monitors {
		section, err := sectionOf(m.Section)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			"INSERT INTO status_page_monitors (page_id, monitor_id, position, display_name, section_id) VALUES (?, ?, ?, ?, ?)",
			pageID, m.MonitorID, position, m.DisplayName, section)
		if err != nil {
			return err
		}
	}
	for position, r := range p.Rules {
		section, err := sectionOf(r.Section)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			"INSERT INTO status_page_rules (page_id, section_id, namespace, tag, position) VALUES (?, ?, ?, ?, ?)",
			pageID, section, r.Namespace, r.Tag, position)
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
		`INSERT INTO status_pages (slug, title, description, announcement, published, noindex, hide_paused, language, detail_pages, password_hash, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Slug, p.Title, p.Description, p.Announcement, p.Published, p.NoIndex, p.HidePaused, p.Language, p.DetailPages, p.PasswordHash, p.CreatedAt.UnixMilli())
	if isUniqueViolation(err) {
		return StatusPage{}, ErrConflict
	}
	if err != nil {
		return StatusPage{}, fmt.Errorf("create status page: %w", err)
	}
	if p.ID, err = res.LastInsertId(); err != nil {
		return StatusPage{}, fmt.Errorf("create status page: %w", err)
	}
	if err := setStatusPageContents(ctx, tx, p.ID, p); err != nil {
		return StatusPage{}, fmt.Errorf("create status page: %w", err)
	}
	created, err := statusPage(ctx, tx, "id = ?", p.ID)
	if err != nil {
		return StatusPage{}, fmt.Errorf("create status page: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return StatusPage{}, fmt.Errorf("create status page: %w", err)
	}
	return created, nil
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
		`UPDATE status_pages SET slug = ?, title = ?, description = ?, announcement = ?, published = ?,
		     noindex = ?, hide_paused = ?, language = ?, detail_pages = ?, password_hash = ? WHERE id = ?`,
		p.Slug, p.Title, p.Description, p.Announcement, p.Published, p.NoIndex, p.HidePaused, p.Language, p.DetailPages, p.PasswordHash, p.ID)
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
	if err := setStatusPageContents(ctx, tx, p.ID, p); err != nil {
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

// inList returns the placeholders of an IN list for ids, and ids as
// arguments.
func inList(ids []int64) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return "?" + strings.Repeat(", ?", len(ids)-1), args
}

func (s *SQLite) DailyUptime(ctx context.Context, monitorIDs []int64, from, to time.Time) (map[int64][]SeriesPoint, error) {
	out := make(map[int64][]SeriesPoint)
	if len(monitorIDs) == 0 {
		return out, nil
	}
	day := (24 * time.Hour).Milliseconds()
	in, args := inList(monitorIDs)
	rows, err := s.read.QueryContext(ctx,
		`SELECT monitor_id, (bucket_start / ?) * ? AS day, SUM(checks), SUM(up_checks)
		 FROM check_rollups
		 WHERE monitor_id IN (`+in+`) AND bucket_start >= ? AND bucket_start < ?
		 GROUP BY monitor_id, day ORDER BY monitor_id, day`,
		append([]any{day, day}, append(args, from.UnixMilli(), to.UnixMilli())...)...)
	if err != nil {
		return nil, fmt.Errorf("daily uptime: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			monitorID, start int64
			p                SeriesPoint
		)
		if err := rows.Scan(&monitorID, &start, &p.Checks, &p.UpChecks); err != nil {
			return nil, fmt.Errorf("daily uptime: %w", err)
		}
		p.Start = time.UnixMilli(start).UTC()
		out[monitorID] = append(out[monitorID], p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("daily uptime: %w", err)
	}
	return out, nil
}

// UptimeWindows reads every window in one pass over the summaries of the
// longest: each hour is counted towards the windows it falls in.
func (s *SQLite) UptimeWindows(ctx context.Context, monitorIDs []int64, now time.Time, windows []time.Duration) (map[int64][]UptimeCount, error) {
	out := make(map[int64][]UptimeCount)
	if len(monitorIDs) == 0 || len(windows) == 0 {
		return out, nil
	}
	in, ids := inList(monitorIDs)
	// The arguments in the order of their placeholders: the cutoffs of the
	// sums, then the monitors, then the bounds of the scan.
	var (
		sums   []string
		args   = make([]any, 0, 2*len(windows)+len(ids)+2)
		oldest = now
	)
	for _, w := range windows {
		from := now.Add(-w).Truncate(RollupBucket)
		if from.Before(oldest) {
			oldest = from
		}
		sums = append(sums, "SUM(CASE WHEN bucket_start >= ? THEN checks ELSE 0 END), SUM(CASE WHEN bucket_start >= ? THEN up_checks ELSE 0 END)")
		args = append(args, from.UnixMilli(), from.UnixMilli())
	}
	args = append(args, ids...)
	args = append(args, oldest.UnixMilli(), now.UnixMilli())
	rows, err := s.read.QueryContext(ctx,
		"SELECT monitor_id, "+strings.Join(sums, ", ")+" FROM check_rollups WHERE monitor_id IN ("+in+
			") AND bucket_start >= ? AND bucket_start < ? GROUP BY monitor_id", args...)
	if err != nil {
		return nil, fmt.Errorf("uptime windows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var monitorID int64
		counts := make([]UptimeCount, len(windows))
		dest := make([]any, 0, 1+2*len(windows))
		dest = append(dest, &monitorID)
		for i := range counts {
			dest = append(dest, &counts[i].Checks, &counts[i].UpChecks)
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("uptime windows: %w", err)
		}
		out[monitorID] = counts
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("uptime windows: %w", err)
	}
	return out, nil
}

const updateColumns = "id, page_id, incident_id, kind, title, body, created_at, created_by, updated_at"

func scanUpdate(row rowScanner) (StatusPageUpdate, error) {
	var (
		u                StatusPageUpdate
		incidentID       sql.NullInt64
		created, updated int64
	)
	if err := row.Scan(&u.ID, &u.PageID, &incidentID, &u.Kind, &u.Title, &u.Body, &created, &u.CreatedBy, &updated); err != nil {
		return StatusPageUpdate{}, err
	}
	u.IncidentID = incidentID.Int64
	u.CreatedAt, u.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	return u, nil
}

// nullID is the column value of an id that may be none.
func nullID(id int64) sql.NullInt64 {
	return sql.NullInt64{Int64: id, Valid: id != 0}
}

// CreateStatusPageUpdate writes the update only while its page exists,
// which the insert itself checks.
func (s *SQLite) CreateStatusPageUpdate(ctx context.Context, u StatusPageUpdate) (StatusPageUpdate, error) {
	u.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
	u.UpdatedAt = u.CreatedAt
	res, err := s.write.ExecContext(ctx,
		`INSERT INTO status_page_updates (page_id, incident_id, kind, title, body, created_at, created_by, updated_at)
		 SELECT p.id, ?, ?, ?, ?, ?, ?, ? FROM status_pages p WHERE p.id = ?`,
		nullID(u.IncidentID), u.Kind, u.Title, u.Body, u.CreatedAt.UnixMilli(), u.CreatedBy, u.UpdatedAt.UnixMilli(), u.PageID)
	if err != nil {
		return StatusPageUpdate{}, fmt.Errorf("create status page update: %w", err)
	}
	if rows, err := res.RowsAffected(); err != nil {
		return StatusPageUpdate{}, fmt.Errorf("create status page update: %w", err)
	} else if rows == 0 {
		return StatusPageUpdate{}, ErrNotFound // the page is gone
	}
	if u.ID, err = res.LastInsertId(); err != nil {
		return StatusPageUpdate{}, fmt.Errorf("create status page update: %w", err)
	}
	created, err := scanUpdate(s.write.QueryRowContext(ctx, "SELECT "+updateColumns+" FROM status_page_updates WHERE id = ?", u.ID))
	if err != nil {
		return StatusPageUpdate{}, fmt.Errorf("create status page update: %w", err)
	}
	return created, nil
}

func (s *SQLite) GetStatusPageUpdate(ctx context.Context, id int64) (StatusPageUpdate, error) {
	u, err := scanUpdate(s.read.QueryRowContext(ctx, "SELECT "+updateColumns+" FROM status_page_updates WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return StatusPageUpdate{}, ErrNotFound
	}
	if err != nil {
		return StatusPageUpdate{}, fmt.Errorf("get status page update: %w", err)
	}
	return u, nil
}

func (s *SQLite) ListStatusPageUpdates(ctx context.Context, pageID int64, limit int) ([]StatusPageUpdate, error) {
	var updates []StatusPageUpdate
	err := eachRow(ctx, s.read,
		"SELECT "+updateColumns+" FROM status_page_updates WHERE page_id = ? ORDER BY created_at DESC, id DESC LIMIT ?",
		func(rows *sql.Rows) error {
			u, err := scanUpdate(rows)
			if err != nil {
				return err
			}
			updates = append(updates, u)
			return nil
		}, pageID, limit)
	if err != nil {
		return nil, fmt.Errorf("list status page updates: %w", err)
	}
	return updates, nil
}

func (s *SQLite) UpdateStatusPageUpdate(ctx context.Context, u StatusPageUpdate) (StatusPageUpdate, error) {
	res, err := s.write.ExecContext(ctx,
		"UPDATE status_page_updates SET incident_id = ?, kind = ?, title = ?, body = ?, updated_at = ? WHERE id = ?",
		nullID(u.IncidentID), u.Kind, u.Title, u.Body, time.Now().UTC().UnixMilli(), u.ID)
	if err != nil {
		return StatusPageUpdate{}, fmt.Errorf("update status page update: %w", err)
	}
	if rows, err := res.RowsAffected(); err != nil {
		return StatusPageUpdate{}, fmt.Errorf("update status page update: %w", err)
	} else if rows == 0 {
		return StatusPageUpdate{}, ErrNotFound
	}
	updated, err := scanUpdate(s.write.QueryRowContext(ctx, "SELECT "+updateColumns+" FROM status_page_updates WHERE id = ?", u.ID))
	if err != nil {
		return StatusPageUpdate{}, fmt.Errorf("update status page update: %w", err)
	}
	return updated, nil
}

func (s *SQLite) DeleteStatusPageUpdate(ctx context.Context, id int64) error {
	res, err := s.write.ExecContext(ctx, "DELETE FROM status_page_updates WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete status page update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete status page update: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) ListStatusPageNotes(ctx context.Context, monitorIDs []int64, limit int) ([]StatusPageNote, error) {
	if len(monitorIDs) == 0 {
		return nil, nil
	}
	in, args := inList(monitorIDs)
	var notes []StatusPageNote
	err := eachRow(ctx, s.read,
		`SELECT n.id, n.incident_id, n.author_id, n.author, n.text, n.show_on_status_page, n.created_at, n.updated_at, i.monitor_id
		 FROM incident_notes n JOIN incidents i ON i.id = n.incident_id
		 WHERE n.show_on_status_page AND i.monitor_id IN (`+in+`)
		 ORDER BY n.created_at DESC, n.id DESC LIMIT ?`,
		func(rows *sql.Rows) error {
			var (
				n                StatusPageNote
				authorID         sql.NullInt64
				created, updated int64
			)
			if err := rows.Scan(&n.ID, &n.IncidentID, &authorID, &n.Author, &n.Text, &n.ShowOnStatusPage, &created, &updated, &n.MonitorID); err != nil {
				return err
			}
			n.AuthorID = authorID.Int64
			n.CreatedAt, n.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
			notes = append(notes, n)
			return nil
		}, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("list status page notes: %w", err)
	}
	return notes, nil
}
