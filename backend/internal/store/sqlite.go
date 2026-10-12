package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// SQLite is a Store backed by a single SQLite file.
//
// SQLite allows one writer at a time, so writes go through a dedicated
// single-connection pool while reads use a separate pool; WAL mode lets
// readers proceed during a write.
type SQLite struct {
	write *sql.DB
	read  *sql.DB
}

var _ Store = (*SQLite)(nil)

// OpenSQLite opens (creating if needed) the database at path and applies
// pending migrations. busyTimeout is how long a connection waits for a lock
// held by another connection before giving up.
func OpenSQLite(ctx context.Context, path string, busyTimeout time.Duration) (*SQLite, error) {
	write, err := openPool(path, busyTimeout, true)
	if err != nil {
		return nil, err
	}
	write.SetMaxOpenConns(1)

	if err := migrate(ctx, write); err != nil {
		write.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	read, err := openPool(path, busyTimeout, false)
	if err != nil {
		write.Close()
		return nil, err
	}
	return &SQLite{write: write, read: read}, nil
}

func openPool(path string, busyTimeout time.Duration, writer bool) (*sql.DB, error) {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
	q.Add("_pragma", "foreign_keys(1)")
	if writer {
		// Take the write lock up front instead of failing on lock upgrade.
		q.Set("_txlock", "immediate")
	} else {
		q.Add("_pragma", "query_only(1)")
	}

	dsn := (&url.URL{Scheme: "file", Opaque: url.PathEscape(path), RawQuery: q.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return db, nil
}

// migrate applies the embedded migrations in file name order. The number of
// applied migrations is tracked in PRAGMA user_version.
func migrate(ctx context.Context, db *sql.DB) error {
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)

	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(names) {
		return fmt.Errorf("database is at version %d, newer than this binary (%d)", version, len(names))
	}

	for i := version; i < len(names); i++ {
		body, err := migrationFiles.ReadFile(names[i])
		if err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", names[i], err)
		}
		// PRAGMA does not accept bound parameters.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Close closes both connection pools.
func (s *SQLite) Close() error {
	return errors.Join(s.read.Close(), s.write.Close())
}

const monitorColumns = "id, name, type, target, interval_seconds, timeout_seconds, failure_threshold, enabled, created_at"

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMonitor(row rowScanner) (Monitor, error) {
	var (
		m                 Monitor
		interval, timeout int64
		createdAt         int64
	)
	err := row.Scan(&m.ID, &m.Name, &m.Type, &m.Target, &interval, &timeout, &m.FailureThreshold, &m.Enabled, &createdAt)
	if err != nil {
		return Monitor{}, err
	}
	m.Interval = time.Duration(interval) * time.Second
	m.Timeout = time.Duration(timeout) * time.Second
	m.CreatedAt = time.UnixMilli(createdAt).UTC()
	return m, nil
}

func (s *SQLite) CreateMonitor(ctx context.Context, m Monitor) (Monitor, error) {
	m.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
	res, err := s.write.ExecContext(ctx,
		`INSERT INTO monitors (name, type, target, interval_seconds, timeout_seconds, failure_threshold, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Name, m.Type, m.Target, int64(m.Interval/time.Second), int64(m.Timeout/time.Second), m.FailureThreshold, m.Enabled, m.CreatedAt.UnixMilli())
	if isUniqueViolation(err) {
		return Monitor{}, ErrConflict
	}
	if err != nil {
		return Monitor{}, fmt.Errorf("create monitor: %w", err)
	}
	if m.ID, err = res.LastInsertId(); err != nil {
		return Monitor{}, fmt.Errorf("create monitor: %w", err)
	}
	return m, nil
}

func (s *SQLite) GetMonitor(ctx context.Context, id int64) (Monitor, error) {
	m, err := scanMonitor(s.read.QueryRowContext(ctx,
		"SELECT "+monitorColumns+" FROM monitors WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Monitor{}, ErrNotFound
	}
	if err != nil {
		return Monitor{}, fmt.Errorf("get monitor: %w", err)
	}
	return m, nil
}

func (s *SQLite) ListMonitors(ctx context.Context) ([]Monitor, error) {
	rows, err := s.read.QueryContext(ctx, "SELECT "+monitorColumns+" FROM monitors ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}
	defer rows.Close()

	var monitors []Monitor
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, fmt.Errorf("list monitors: %w", err)
		}
		monitors = append(monitors, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}
	return monitors, nil
}

// isUniqueViolation reports whether err is a UNIQUE constraint failure.
func isUniqueViolation(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}

// statusQuery joins each monitor with its newest result (found through the
// (monitor_id, checked_at) index) and with its aggregates since a cutoff.
const statusQuery = `
SELECT m.id, m.name, m.type, m.target, m.interval_seconds, m.timeout_seconds, m.failure_threshold, m.enabled, m.created_at,
       r.checked_at, r.up, r.latency_ms, r.status_code, r.error,
       COALESCE(a.checks, 0), COALESCE(a.up_checks, 0), COALESCE(a.avg_latency_ms, 0),
       i.id, i.started_at, i.cause
FROM monitors m
LEFT JOIN incidents i ON i.monitor_id = m.id AND i.resolved_at IS NULL
LEFT JOIN check_results r ON r.id = (
    SELECT id FROM check_results
    WHERE monitor_id = m.id
    ORDER BY checked_at DESC, id DESC LIMIT 1)
LEFT JOIN (
    SELECT monitor_id, COUNT(*) AS checks, SUM(up) AS up_checks, AVG(latency_ms) AS avg_latency_ms
    FROM check_results
    WHERE checked_at >= ?1
    GROUP BY monitor_id) a ON a.monitor_id = m.id
WHERE ?2 = 0 OR m.id = ?2
ORDER BY m.id`

func (s *SQLite) ListMonitorStatuses(ctx context.Context, since time.Time) ([]MonitorStatus, error) {
	statuses, err := s.queryStatuses(ctx, since, 0)
	if err != nil {
		return nil, fmt.Errorf("list monitor statuses: %w", err)
	}
	return statuses, nil
}

func (s *SQLite) GetMonitorStatus(ctx context.Context, id int64, since time.Time) (MonitorStatus, error) {
	statuses, err := s.queryStatuses(ctx, since, id)
	if err != nil {
		return MonitorStatus{}, fmt.Errorf("get monitor status: %w", err)
	}
	if len(statuses) == 0 {
		return MonitorStatus{}, ErrNotFound
	}
	return statuses[0], nil
}

// queryStatuses runs statusQuery for one monitor, or for all when id is 0.
func (s *SQLite) queryStatuses(ctx context.Context, since time.Time, id int64) ([]MonitorStatus, error) {
	rows, err := s.read.QueryContext(ctx, statusQuery, since.UnixMilli(), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var statuses []MonitorStatus
	for rows.Next() {
		var (
			st                           MonitorStatus
			interval, timeout, createdAt int64
			checkedAt, latencyMS         sql.NullInt64
			up                           sql.NullBool
			statusCode                   sql.NullInt64
			errText                      sql.NullString
			avgLatencyMS                 float64
			incidentID, incidentStart    sql.NullInt64
			incidentCause                sql.NullString
		)
		err := rows.Scan(&st.ID, &st.Name, &st.Type, &st.Target, &interval, &timeout, &st.FailureThreshold, &st.Enabled, &createdAt,
			&checkedAt, &up, &latencyMS, &statusCode, &errText,
			&st.Checks, &st.UpChecks, &avgLatencyMS,
			&incidentID, &incidentStart, &incidentCause)
		if err != nil {
			return nil, err
		}
		st.Interval = time.Duration(interval) * time.Second
		st.Timeout = time.Duration(timeout) * time.Second
		st.CreatedAt = time.UnixMilli(createdAt).UTC()
		st.AvgLatency = time.Duration(avgLatencyMS * float64(time.Millisecond))
		if checkedAt.Valid {
			st.Last = &CheckResult{
				MonitorID:  st.ID,
				CheckedAt:  time.UnixMilli(checkedAt.Int64).UTC(),
				Up:         up.Bool,
				Latency:    time.Duration(latencyMS.Int64) * time.Millisecond,
				StatusCode: int(statusCode.Int64),
				Error:      errText.String,
			}
		}
		if incidentID.Valid {
			st.Incident = &Incident{
				ID:          incidentID.Int64,
				MonitorID:   st.ID,
				MonitorName: st.Name,
				StartedAt:   time.UnixMilli(incidentStart.Int64).UTC(),
				Cause:       incidentCause.String,
			}
		}
		statuses = append(statuses, st)
	}
	return statuses, rows.Err()
}

func (s *SQLite) DeleteMonitor(ctx context.Context, id int64) error {
	res, err := s.write.ExecContext(ctx, "DELETE FROM monitors WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete monitor: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete monitor: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) InsertCheckResults(ctx context.Context, results []CheckResult) error {
	if len(results) == 0 {
		return nil
	}
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("insert check results: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO check_results (monitor_id, checked_at, up, latency_ms, status_code, error)
		 VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("insert check results: %w", err)
	}
	defer stmt.Close()

	for _, r := range results {
		var (
			statusCode sql.NullInt64
			errText    sql.NullString
		)
		if r.StatusCode != 0 {
			statusCode = sql.NullInt64{Int64: int64(r.StatusCode), Valid: true}
		}
		if r.Error != "" {
			errText = sql.NullString{String: r.Error, Valid: true}
		}
		_, err := stmt.ExecContext(ctx, r.MonitorID, r.CheckedAt.UnixMilli(), r.Up, r.Latency.Milliseconds(), statusCode, errText)
		if err != nil {
			return fmt.Errorf("insert check results: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("insert check results: %w", err)
	}
	return nil
}

func (s *SQLite) ListCheckResults(ctx context.Context, monitorID int64, limit int) ([]CheckResult, error) {
	rows, err := s.read.QueryContext(ctx,
		`SELECT monitor_id, checked_at, up, latency_ms, status_code, error
		 FROM check_results WHERE monitor_id = ?
		 ORDER BY checked_at DESC, id DESC LIMIT ?`, monitorID, limit)
	if err != nil {
		return nil, fmt.Errorf("list check results: %w", err)
	}
	defer rows.Close()

	var results []CheckResult
	for rows.Next() {
		var (
			r                    CheckResult
			checkedAt, latencyMS int64
			statusCode           sql.NullInt64
			errText              sql.NullString
		)
		if err := rows.Scan(&r.MonitorID, &checkedAt, &r.Up, &latencyMS, &statusCode, &errText); err != nil {
			return nil, fmt.Errorf("list check results: %w", err)
		}
		r.CheckedAt = time.UnixMilli(checkedAt).UTC()
		r.Latency = time.Duration(latencyMS) * time.Millisecond
		r.StatusCode = int(statusCode.Int64)
		r.Error = errText.String
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list check results: %w", err)
	}
	return results, nil
}

func (s *SQLite) DeleteCheckResultsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.write.ExecContext(ctx, "DELETE FROM check_results WHERE checked_at < ?", cutoff.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("delete check results: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete check results: %w", err)
	}
	return n, nil
}
