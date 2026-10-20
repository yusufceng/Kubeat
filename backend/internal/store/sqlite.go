package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/yusufceng/Kubeat/backend/internal/secretbox"
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
	// path and busyTimeout are kept to open extra connections for backups.
	path        string
	busyTimeout time.Duration
	// secrets encrypts the secrets of monitors and channels; nil stores
	// them as they are.
	secrets *secretbox.Box
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
	return &SQLite{write: write, read: read, path: path, busyTimeout: busyTimeout}, nil
}

// Backup writes a consistent, compacted copy of the database to dest, which
// must not exist. It runs while the database is in use: the copy reflects
// the moment it started, and writers are not held up.
func (s *SQLite) Backup(ctx context.Context, dest string) error {
	// A connection of its own, so the single write connection stays free for
	// check results while a large database is being copied.
	db, err := openPool(s.path, s.busyTimeout, true)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	// VACUUM INTO takes no bound parameters; the path is quoted as a literal.
	quoted := "'" + strings.ReplaceAll(dest, "'", "''") + "'"
	if _, err := db.ExecContext(ctx, "VACUUM INTO "+quoted); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return nil
}

// Vacuum rewrites the database file without its free space, where deleted
// and overwritten values stay readable unless they were zeroed when they
// went, and empties the write-ahead log, which holds earlier versions of
// the pages too. Once both are done, a compaction owed (see
// SetCompactionPending) is no longer. The rewrite holds the write lock and
// needs free disk about the size of the database, so it is for occasions,
// not routine.
func (s *SQLite) Vacuum(ctx context.Context) error {
	_, vacuumErr := s.write.ExecContext(ctx, "VACUUM")
	// The log is emptied also when VACUUM failed or was cut short: one that
	// ran out of disk leaves it grown by about the size of the database.
	// What a failed VACUUM wrote was not committed, so this is quick.
	checkpointErr := s.checkpoint(context.WithoutCancel(ctx))
	if vacuumErr != nil {
		return errors.Join(fmt.Errorf("vacuum: %w", vacuumErr), checkpointErr)
	}
	if checkpointErr != nil {
		return checkpointErr
	}
	if _, err := s.write.ExecContext(ctx, "DELETE FROM compaction_pending"); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}
	return nil
}

// checkpoint copies the write-ahead log into the database and empties it.
func (s *SQLite) checkpoint(ctx context.Context) error {
	var busy, frames, checkpointed int
	if err := s.write.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &checkpointed); err != nil {
		return fmt.Errorf("vacuum: checkpoint: %w", err)
	}
	if busy != 0 {
		return errors.New("vacuum: checkpoint: a reader kept the write-ahead log from being emptied")
	}
	return nil
}

// SetCompactionPending records that the database is owed a compaction, if
// that is not recorded already: call it before writing what must not stay
// behind in the free space of the file, so that a start cut short before
// Vacuum is done leaves the compaction to the next. Vacuum forgets it.
func (s *SQLite) SetCompactionPending(ctx context.Context, since time.Time) error {
	if _, err := s.write.ExecContext(ctx, "INSERT OR IGNORE INTO compaction_pending (id, since) VALUES (1, ?)", since.UnixMilli()); err != nil {
		return fmt.Errorf("record a pending compaction: %w", err)
	}
	return nil
}

// PendingCompaction is a compaction the database is owed: since when, and
// how many starts in a row tried it and did not finish.
type PendingCompaction struct {
	Since    time.Time
	Attempts int
}

// CompactionPending reports whether the database is owed a compaction, and
// what is known about it.
func (s *SQLite) CompactionPending(ctx context.Context) (PendingCompaction, bool, error) {
	var at int64
	var p PendingCompaction
	err := s.read.QueryRowContext(ctx, "SELECT since, attempts FROM compaction_pending").Scan(&at, &p.Attempts)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return PendingCompaction{}, false, nil
	case err != nil:
		return PendingCompaction{}, false, fmt.Errorf("read a pending compaction: %w", err)
	}
	p.Since = time.UnixMilli(at).UTC()
	return p, true, nil
}

// SetCompactionAttempts records how many starts in a row tried the
// compaction owed and did not finish it, if one is owed. A start counts
// its own attempt before Vacuum, so that being cut short during it still
// counts.
func (s *SQLite) SetCompactionAttempts(ctx context.Context, attempts int) error {
	if _, err := s.write.ExecContext(ctx, "UPDATE compaction_pending SET attempts = ?", attempts); err != nil {
		return fmt.Errorf("count a compaction attempt: %w", err)
	}
	return nil
}

// SchemaVersion returns the number of migrations applied to the database
// file at path, without modifying it.
func SchemaVersion(ctx context.Context, path string) (int, error) {
	q := url.Values{}
	q.Set("mode", "ro")
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Opaque: url.PathEscape(path), RawQuery: q.Encode()}).String())
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, err
	}
	// Reading the schema fails on a file that is not a usable database.
	var tables int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'").Scan(&tables); err != nil {
		return 0, err
	}
	return version, nil
}

// CurrentSchemaVersion is the schema version this binary creates.
func CurrentSchemaVersion() int {
	names, _ := fs.Glob(migrationFiles, "migrations/*.sql")
	return len(names)
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
		// What is deleted or overwritten is zeroed in the file instead of
		// staying readable in its free space: an old value of a secret,
		// or one stored before a key was configured, is gone with its row.
		q.Add("_pragma", "secure_delete(1)")
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

// withoutForeignKeys, as the first line of a migration, runs it with foreign
// key enforcement off. A migration that rebuilds a table needs that:
// dropping the old table would otherwise delete every row referring to it.
const withoutForeignKeys = "-- migrate: foreign_keys=off"

// migrate applies the embedded migrations in file name order. The number of
// applied migrations is tracked in PRAGMA user_version.
func migrate(ctx context.Context, db *sql.DB) error {
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)

	// One connection throughout: the foreign key setting belongs to it.
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
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
		if err := applyMigration(ctx, conn, string(body), i+1); err != nil {
			return fmt.Errorf("%s: %w", names[i], err)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, conn *sql.Conn, body string, version int) (err error) {
	rebuild := strings.HasPrefix(body, withoutForeignKeys)
	if rebuild {
		// The setting cannot be changed inside a transaction.
		if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
			return err
		}
		defer func() {
			if _, onErr := conn.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err == nil {
				err = onErr
			}
		}()
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, body); err != nil {
		return err
	}
	if rebuild {
		// Nothing enforced the references meanwhile, so check them all.
		rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
		if err != nil {
			return err
		}
		broken := rows.Next()
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		if broken {
			return errors.New("the migration left rows that refer to missing rows")
		}
	}
	// PRAGMA does not accept bound parameters.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return err
	}
	return tx.Commit()
}

// Close closes both connection pools.
func (s *SQLite) Close() error {
	return errors.Join(s.read.Close(), s.write.Close())
}

// monitorFields are the columns of a monitor, in the order monitorRow reads them.
var monitorFields = []string{
	"id", "name", "description", "type", "target", "interval_seconds", "timeout_seconds", "retry_interval_seconds",
	"failure_threshold", "inverted", "enabled", "push_token", "created_at", "namespace", "source", "tags", "settings", "managed", "each_pod",
}

var monitorColumns = strings.Join(monitorFields, ", ")

// monitorColumnsOf returns the monitor columns qualified with a table alias.
func monitorColumnsOf(alias string) string {
	return alias + "." + strings.Join(monitorFields, ", "+alias+".")
}

type rowScanner interface {
	Scan(dest ...any) error
}

// monitorRow receives the monitor columns of a row.
type monitorRow struct {
	m                                   Monitor
	interval, timeout, retry, createdAt int64
	tags, settings                      string
	// box decrypts the monitor's secrets. With raw set they are left in
	// their stored form instead.
	box *secretbox.Box
	raw bool
}

// dest returns where the monitor columns are scanned to.
func (r *monitorRow) dest() []any {
	return []any{
		&r.m.ID, &r.m.Name, &r.m.Description, &r.m.Type, &r.m.Target, &r.interval, &r.timeout, &r.retry,
		&r.m.FailureThreshold, &r.m.Inverted, &r.m.Enabled, &r.m.PushToken, &r.createdAt, &r.m.Namespace, &r.m.Source, &r.tags, &r.settings, &r.m.Managed, &r.m.EachPod,
	}
}

// monitor returns the scanned monitor.
func (r *monitorRow) monitor() (Monitor, error) {
	m := r.m
	m.Interval = time.Duration(r.interval) * time.Second
	m.Timeout = time.Duration(r.timeout) * time.Second
	m.RetryInterval = time.Duration(r.retry) * time.Second
	m.CreatedAt = time.UnixMilli(r.createdAt).UTC()
	if err := decodeSettings(r.settings, &m); err != nil {
		return Monitor{}, fmt.Errorf("monitor %d: %w", m.ID, err)
	}
	if !r.raw {
		if err := monitorSecrets(&m, r.box.Open); err != nil {
			return Monitor{}, fmt.Errorf("monitor %d: %w", m.ID, err)
		}
	}
	if err := json.Unmarshal([]byte(r.tags), &m.Tags); err != nil {
		return Monitor{}, fmt.Errorf("monitor %d: decode tags: %w", m.ID, err)
	}
	if len(m.Tags) == 0 {
		m.Tags = nil
	}
	return m, nil
}

// encodeTags stores tags as a JSON array, sorted and without duplicates; no
// tags is "[]". It returns the tags in that stored order too.
func encodeTags(tags []string) (string, []string, error) {
	if len(tags) == 0 {
		return "[]", nil, nil
	}
	sorted := slices.Clone(tags)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)
	b, err := json.Marshal(sorted)
	return string(b), sorted, err
}

func (s *SQLite) scanMonitor(row rowScanner) (Monitor, error) {
	r := monitorRow{box: s.secrets}
	if err := row.Scan(r.dest()...); err != nil {
		return Monitor{}, err
	}
	return r.monitor()
}

// settings is the stored form of a monitor's check options: only the part
// that belongs to its type is present.
type settings struct {
	HTTP      *HTTPOptions      `json:"http,omitempty"`
	TCP       *TCPOptions       `json:"tcp,omitempty"`
	DNS       *DNSOptions       `json:"dns,omitempty"`
	GRPC      *GRPCOptions      `json:"grpc,omitempty"`
	WebSocket *WebSocketOptions `json:"websocket,omitempty"`
	Database  *DatabaseOptions  `json:"database,omitempty"`
	Workload  *WorkloadOptions  `json:"workload,omitempty"`
}

// normalize fills in the defaults of the monitor's check options and clears
// the options of other types, so that what is returned to a caller equals
// what a later read returns.
func normalize(m *Monitor) {
	kept := Monitor{}
	switch m.Type {
	case MonitorHTTP:
		kept.HTTP = m.HTTP
		if kept.HTTP.Method == "" {
			kept.HTTP.Method = DefaultHTTPMethod
		}
		if kept.HTTP.ExpectedStatus == "" {
			kept.HTTP.ExpectedStatus = DefaultExpectedStatus
		}
		if len(kept.HTTP.Headers) == 0 {
			kept.HTTP.Headers = nil
		}
	case MonitorTCP:
		kept.TCP = m.TCP
	case MonitorDNS:
		kept.DNS = m.DNS
		if kept.DNS.RecordType == "" {
			kept.DNS.RecordType = DefaultDNSRecordType
		}
	case MonitorGRPC:
		kept.GRPC = m.GRPC
	case MonitorWebSocket:
		kept.WebSocket = m.WebSocket
	case MonitorPostgres, MonitorMySQL:
		kept.Database = m.Database
		if kept.Database.Query == "" {
			kept.Database.Query = DefaultDatabaseQuery
		}
	case MonitorRedis:
		kept.Database = m.Database
		kept.Database.Database, kept.Database.Query = "", ""
	case MonitorWorkload:
		kept.Workload = m.Workload
	}
	m.HTTP, m.TCP, m.DNS, m.GRPC, m.WebSocket = kept.HTTP, kept.TCP, kept.DNS, kept.GRPC, kept.WebSocket
	m.Database, m.Workload = kept.Database, kept.Workload
}

// encodeSettings returns the stored form of a normalized monitor's options.
func encodeSettings(m Monitor) (string, error) {
	var st settings
	switch m.Type {
	case MonitorHTTP:
		st.HTTP = &m.HTTP
	case MonitorTCP:
		st.TCP = &m.TCP
	case MonitorDNS:
		st.DNS = &m.DNS
	case MonitorGRPC:
		st.GRPC = &m.GRPC
	case MonitorWebSocket:
		st.WebSocket = &m.WebSocket
	case MonitorPostgres, MonitorMySQL, MonitorRedis:
		st.Database = &m.Database
	case MonitorWorkload:
		st.Workload = &m.Workload
	}
	b, err := json.Marshal(st)
	return string(b), err
}

// monitorSecrets replaces each secret of the monitor's options with what
// convert makes of it: sealing them to store, opening them when read. The
// request body counts as one: it often carries a token or a password, of a
// login or a GraphQL query. A value stored plain, before it was counted
// among the secrets, opens as it is.
func monitorSecrets(m *Monitor, convert func(string) (string, error)) error {
	var err error
	if m.HTTP.AuthSecret, err = convert(m.HTTP.AuthSecret); err != nil {
		return err
	}
	if m.HTTP.Body, err = convert(m.HTTP.Body); err != nil {
		return err
	}
	if m.Database.Password, err = convert(m.Database.Password); err != nil {
		return err
	}
	if len(m.HTTP.Headers) > 0 {
		// A map of its own: the caller's must keep its plain values.
		headers := make(map[string]string, len(m.HTTP.Headers))
		for name, value := range m.HTTP.Headers {
			if headers[name], err = convert(value); err != nil {
				return err
			}
		}
		m.HTTP.Headers = headers
	}
	return nil
}

// storedSettings returns the stored form of a normalized monitor's options,
// with its secrets encrypted.
func (s *SQLite) storedSettings(m Monitor) (string, error) {
	if err := monitorSecrets(&m, s.secrets.Seal); err != nil {
		return "", err
	}
	return encodeSettings(m)
}

func decodeSettings(text string, m *Monitor) error {
	st := settings{HTTP: &m.HTTP, TCP: &m.TCP, DNS: &m.DNS, GRPC: &m.GRPC, WebSocket: &m.WebSocket, Database: &m.Database, Workload: &m.Workload}
	if err := json.Unmarshal([]byte(text), &st); err != nil {
		return fmt.Errorf("decode settings: %w", err)
	}
	normalize(m)
	return nil
}

func (s *SQLite) CreateMonitor(ctx context.Context, m Monitor) (Monitor, error) {
	if !slices.Contains(MonitorTypes, m.Type) {
		return Monitor{}, fmt.Errorf("create monitor: unknown type %q", m.Type)
	}
	normalize(&m)
	encoded, err := s.storedSettings(m)
	if err != nil {
		return Monitor{}, fmt.Errorf("create monitor: %w", err)
	}
	tags, sorted, err := encodeTags(m.Tags)
	if err != nil {
		return Monitor{}, fmt.Errorf("create monitor: %w", err)
	}
	m.Tags = sorted
	m.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)

	res, err := s.write.ExecContext(ctx,
		`INSERT INTO monitors (name, description, type, target, interval_seconds, timeout_seconds, retry_interval_seconds,
		                       failure_threshold, inverted, enabled, push_token, created_at, namespace, source, tags, settings, managed, each_pod)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Name, m.Description, m.Type, m.Target, int64(m.Interval/time.Second), int64(m.Timeout/time.Second), int64(m.RetryInterval/time.Second),
		m.FailureThreshold, m.Inverted, m.Enabled, m.PushToken, m.CreatedAt.UnixMilli(), m.Namespace, m.Source, tags, encoded, m.Managed, m.EachPod)
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
	m, err := s.scanMonitor(s.read.QueryRowContext(ctx,
		"SELECT "+monitorColumns+" FROM monitors WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Monitor{}, ErrNotFound
	}
	if err != nil {
		return Monitor{}, fmt.Errorf("get monitor: %w", err)
	}
	return m, nil
}

func (s *SQLite) GetMonitorByPushToken(ctx context.Context, token string) (Monitor, error) {
	if token == "" {
		return Monitor{}, ErrNotFound
	}
	m, err := s.scanMonitor(s.read.QueryRowContext(ctx,
		"SELECT "+monitorColumns+" FROM monitors WHERE push_token = ?", token))
	if errors.Is(err, sql.ErrNoRows) {
		return Monitor{}, ErrNotFound
	}
	if err != nil {
		return Monitor{}, fmt.Errorf("get monitor by push token: %w", err)
	}
	return m, nil
}

func (s *SQLite) SetPushToken(ctx context.Context, id int64, token string) error {
	res, err := s.write.ExecContext(ctx, "UPDATE monitors SET push_token = ? WHERE id = ? AND type = ?", token, id, MonitorPush)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("set push token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set push token: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) RecordPush(ctx context.Context, report PushReport) error {
	_, err := s.write.ExecContext(ctx,
		`INSERT INTO push_reports (monitor_id, received_at, up, message, latency_ms) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (monitor_id) DO UPDATE SET
		     received_at = excluded.received_at, up = excluded.up, message = excluded.message, latency_ms = excluded.latency_ms`,
		report.MonitorID, report.ReceivedAt.UnixMilli(), report.Up, report.Message, report.Latency.Milliseconds())
	if err != nil {
		return fmt.Errorf("record push: %w", err)
	}
	return nil
}

func (s *SQLite) LastPush(ctx context.Context, monitorID int64) (PushReport, error) {
	report := PushReport{MonitorID: monitorID}
	var receivedAt, latencyMS int64
	err := s.read.QueryRowContext(ctx,
		"SELECT received_at, up, message, latency_ms FROM push_reports WHERE monitor_id = ?", monitorID).
		Scan(&receivedAt, &report.Up, &report.Message, &latencyMS)
	if errors.Is(err, sql.ErrNoRows) {
		return PushReport{}, ErrNotFound
	}
	if err != nil {
		return PushReport{}, fmt.Errorf("last push: %w", err)
	}
	report.ReceivedAt = time.UnixMilli(receivedAt).UTC()
	report.Latency = time.Duration(latencyMS) * time.Millisecond
	return report, nil
}

func (s *SQLite) ListMonitors(ctx context.Context) ([]Monitor, error) {
	rows, err := s.read.QueryContext(ctx, "SELECT "+monitorColumns+" FROM monitors ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}
	defer rows.Close()

	var monitors []Monitor
	for rows.Next() {
		m, err := s.scanMonitor(rows)
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

// unixMilliOrZero converts a nullable timestamp column; NULL is the zero time.
func unixMilliOrZero(v sql.NullInt64) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return time.UnixMilli(v.Int64).UTC()
}

// isUniqueViolation reports whether err is a UNIQUE constraint failure.
func isUniqueViolation(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}

// statusQuery joins each monitor with its newest result (found through the
// (monitor_id, checked_at) index) and with its aggregates since a cutoff.
// The aggregates come from the hourly summaries: computing them from raw
// results means reading every result in the window on every request, which
// took seconds at a few hundred monitors.
var statusQuery = `
SELECT ` + monitorColumnsOf("m") + `,
       r.checked_at, r.up, r.latency_ms, r.status_code, r.error, r.detail, r.cert_expires_at,
       COALESCE(a.checks, 0), COALESCE(a.up_checks, 0), COALESCE(a.up_latency_sum_ms, 0),
       i.id, i.started_at, i.cause, i.opened_at, c.id IS NOT NULL
FROM monitors m
LEFT JOIN incidents i ON i.monitor_id = m.id AND i.kind = 'down' AND i.resolved_at IS NULL
LEFT JOIN incidents c ON c.monitor_id = m.id AND c.kind = 'cert' AND c.resolved_at IS NULL
LEFT JOIN check_results r ON r.id = (
    SELECT id FROM check_results
    WHERE monitor_id = m.id
    ORDER BY checked_at DESC, id DESC LIMIT 1)
LEFT JOIN (
    SELECT monitor_id, SUM(checks) AS checks, SUM(up_checks) AS up_checks, SUM(up_latency_sum_ms) AS up_latency_sum_ms
    FROM check_rollups
    WHERE bucket_start >= ?1
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
	windows, until, err := s.activeMaintenance(ctx, time.Now())
	if err != nil {
		return nil, err
	}
	rows, err := s.read.QueryContext(ctx, statusQuery, since.Truncate(RollupBucket).UnixMilli(), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var statuses []MonitorStatus
	for rows.Next() {
		var (
			row                       = monitorRow{box: s.secrets}
			st                        MonitorStatus
			checkedAt, latencyMS      sql.NullInt64
			up                        sql.NullBool
			statusCode                sql.NullInt64
			errText, detail           sql.NullString
			upLatencySumMS            int64
			incidentID, incidentStart sql.NullInt64
			incidentCause             sql.NullString
			incidentOpened            sql.NullInt64
			certExpires               sql.NullInt64
		)
		err := rows.Scan(append(row.dest(),
			&checkedAt, &up, &latencyMS, &statusCode, &errText, &detail, &certExpires,
			&st.Checks, &st.UpChecks, &upLatencySumMS,
			&incidentID, &incidentStart, &incidentCause, &incidentOpened, &st.CertWarning)...)
		if err != nil {
			return nil, err
		}
		if st.Monitor, err = row.monitor(); err != nil {
			return nil, err
		}
		if st.UpChecks > 0 {
			st.AvgLatency = time.Duration(float64(upLatencySumMS) / float64(st.UpChecks) * float64(time.Millisecond))
		}
		// Of the windows that cover the monitor, the one that ends last.
		for i, w := range windows {
			if w.Covers(st.Monitor) && until[i].After(st.MaintenanceUntil) {
				st.MaintenanceUntil = until[i]
			}
		}
		if checkedAt.Valid {
			st.Last = &CheckResult{
				MonitorID:  st.ID,
				CheckedAt:  time.UnixMilli(checkedAt.Int64).UTC(),
				Up:         up.Bool,
				Latency:    time.Duration(latencyMS.Int64) * time.Millisecond,
				StatusCode: int(statusCode.Int64),
				Error:      errText.String,
				Detail:     detail.String,

				CertExpiresAt: unixMilliOrZero(certExpires),
			}
		}
		if incidentID.Valid {
			st.Incident = &Incident{
				ID:          incidentID.Int64,
				MonitorID:   st.ID,
				Kind:        IncidentDown,
				MonitorName: st.Name,
				StartedAt:   time.UnixMilli(incidentStart.Int64).UTC(),
				Cause:       incidentCause.String,

				MonitorNamespace: st.Namespace,
				OpenedAt:         unixMilliOrZero(incidentOpened),
			}
		}
		statuses = append(statuses, st)
	}
	return statuses, rows.Err()
}

func (s *SQLite) UpdateMonitor(ctx context.Context, m Monitor) (Monitor, error) {
	// The type decides which options are stored, and it never changes; nor
	// does who manages the monitor.
	current, err := s.GetMonitor(ctx, m.ID)
	if err != nil {
		return Monitor{}, err
	}
	m.Type = current.Type
	m.Managed = current.Managed
	if current.Source != "" {
		// A discovered monitor lives where its object does.
		m.Namespace = current.Namespace
	}
	normalize(&m)
	encoded, err := s.storedSettings(m)
	if err != nil {
		return Monitor{}, fmt.Errorf("update monitor: %w", err)
	}
	tags, _, err := encodeTags(m.Tags)
	if err != nil {
		return Monitor{}, fmt.Errorf("update monitor: %w", err)
	}
	res, err := s.write.ExecContext(ctx,
		`UPDATE monitors
		 SET name = ?, description = ?, target = ?, interval_seconds = ?, timeout_seconds = ?, retry_interval_seconds = ?,
		     failure_threshold = ?, inverted = ?, enabled = ?, namespace = ?, tags = ?, settings = ?, each_pod = ?
		 WHERE id = ?`,
		m.Name, m.Description, m.Target, int64(m.Interval/time.Second), int64(m.Timeout/time.Second), int64(m.RetryInterval/time.Second),
		m.FailureThreshold, m.Inverted, m.Enabled, m.Namespace, tags, encoded, m.EachPod, m.ID)
	if isUniqueViolation(err) {
		return Monitor{}, ErrConflict
	}
	if err != nil {
		return Monitor{}, fmt.Errorf("update monitor: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Monitor{}, fmt.Errorf("update monitor: %w", err)
	}
	if n == 0 {
		return Monitor{}, ErrNotFound
	}
	// Read back through the writer so the answer reflects this update.
	updated, err := s.scanMonitor(s.write.QueryRowContext(ctx, "SELECT "+monitorColumns+" FROM monitors WHERE id = ?", m.ID))
	if err != nil {
		return Monitor{}, fmt.Errorf("update monitor: %w", err)
	}
	return updated, nil
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
		`INSERT INTO check_results (monitor_id, checked_at, up, latency_ms, status_code, error, detail, cert_expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("insert check results: %w", err)
	}
	defer stmt.Close()

	for _, r := range results {
		var (
			statusCode  sql.NullInt64
			errText     sql.NullString
			detail      sql.NullString
			certExpires sql.NullInt64
		)
		if r.Detail != "" {
			detail = sql.NullString{String: r.Detail, Valid: true}
		}
		if !r.CertExpiresAt.IsZero() {
			certExpires = sql.NullInt64{Int64: r.CertExpiresAt.UnixMilli(), Valid: true}
		}
		if r.StatusCode != 0 {
			statusCode = sql.NullInt64{Int64: int64(r.StatusCode), Valid: true}
		}
		if r.Error != "" {
			errText = sql.NullString{String: r.Error, Valid: true}
		}
		_, err := stmt.ExecContext(ctx, r.MonitorID, r.CheckedAt.UnixMilli(), r.Up, r.Latency.Milliseconds(), statusCode, errText, detail, certExpires)
		if err != nil {
			return fmt.Errorf("insert check results: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("insert check results: %w", err)
	}
	return nil
}

const recentResultsQuery = `SELECT monitor_id, checked_at, up, latency_ms, status_code, error, detail, cert_expires_at
		 FROM check_results WHERE monitor_id = ?
		 ORDER BY checked_at DESC, id DESC LIMIT ?`

func (s *SQLite) ListCheckResults(ctx context.Context, monitorID int64, limit int) ([]CheckResult, error) {
	rows, err := s.read.QueryContext(ctx, recentResultsQuery, monitorID, limit)
	if err != nil {
		return nil, fmt.Errorf("list check results: %w", err)
	}
	return scanCheckResults(rows)
}

// RecentCheckResults runs one indexed lookup per monitor on a single
// connection and prepared statement, which is far cheaper than a request
// per monitor.
func (s *SQLite) RecentCheckResults(ctx context.Context, monitorIDs []int64, limit int) (map[int64][]CheckResult, error) {
	out := make(map[int64][]CheckResult, len(monitorIDs))
	if len(monitorIDs) == 0 {
		return out, nil
	}
	conn, err := s.read.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("recent check results: %w", err)
	}
	defer conn.Close()
	stmt, err := conn.PrepareContext(ctx, recentResultsQuery)
	if err != nil {
		return nil, fmt.Errorf("recent check results: %w", err)
	}
	defer stmt.Close()

	for _, id := range monitorIDs {
		rows, err := stmt.QueryContext(ctx, id, limit)
		if err != nil {
			return nil, fmt.Errorf("recent check results: %w", err)
		}
		results, err := scanCheckResults(rows)
		if err != nil {
			return nil, err
		}
		out[id] = results
	}
	return out, nil
}

// scanCheckResults reads and closes rows.
func scanCheckResults(rows *sql.Rows) ([]CheckResult, error) {
	defer rows.Close()

	var results []CheckResult
	for rows.Next() {
		var (
			r                    CheckResult
			checkedAt, latencyMS int64
			statusCode           sql.NullInt64
			errText, detail      sql.NullString
			certExpires          sql.NullInt64
		)
		if err := rows.Scan(&r.MonitorID, &checkedAt, &r.Up, &latencyMS, &statusCode, &errText, &detail, &certExpires); err != nil {
			return nil, fmt.Errorf("list check results: %w", err)
		}
		r.CheckedAt = time.UnixMilli(checkedAt).UTC()
		r.Latency = time.Duration(latencyMS) * time.Millisecond
		r.StatusCode = int(statusCode.Int64)
		r.Error = errText.String
		r.Detail = detail.String
		r.CertExpiresAt = unixMilliOrZero(certExpires)
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list check results: %w", err)
	}
	return results, nil
}

// deleteChunk is how many rows one retention statement removes. After
// downtime there can be millions to delete; in one statement that would
// hold the write connection, and with it the storing of new results, for
// the whole time.
const deleteChunk = 20000

func (s *SQLite) DeleteCheckResultsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	n, err := s.deleteBefore(ctx, "check_results", "checked_at", cutoff)
	if err != nil {
		return n, fmt.Errorf("delete check results: %w", err)
	}
	return n, nil
}

// deleteBefore deletes the rows of a table whose time column is before
// cutoff, deleteChunk at a time, and reports how many went. The table and
// the column are names from this package, never input.
func (s *SQLite) deleteBefore(ctx context.Context, table, column string, cutoff time.Time) (int64, error) {
	query := "DELETE FROM " + table + " WHERE id IN (SELECT id FROM " + table + " WHERE " + column + " < ? LIMIT ?)"
	var total int64
	for {
		res, err := s.write.ExecContext(ctx, query, cutoff.UnixMilli(), deleteChunk)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < deleteChunk {
			return total, nil
		}
	}
}
