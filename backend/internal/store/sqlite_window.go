package store

import (
	"context"
	"fmt"
	"time"
)

// CheckSummary counts a monitor's checks in a window of time.
type CheckSummary struct {
	Total, Failed int
	// Over the successful checks; zero when there were none.
	AvgLatency, MaxLatency time.Duration
}

// SummarizeChecks counts the checks of a monitor from `from` (inclusive) to
// `to` (exclusive). Only the raw results answer, so a window older than the
// retention counts nothing.
func (s *SQLite) SummarizeChecks(ctx context.Context, monitorID int64, from, to time.Time) (CheckSummary, error) {
	var (
		sum        CheckSummary
		avg, peak  float64
		avgAnswers bool
	)
	row := s.read.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN up THEN 0 ELSE 1 END), 0),
		        COALESCE(AVG(CASE WHEN up THEN latency_ms END), 0), COALESCE(MAX(CASE WHEN up THEN latency_ms END), 0),
		        COUNT(CASE WHEN up THEN 1 END) > 0
		 FROM check_results WHERE monitor_id = ? AND checked_at >= ? AND checked_at < ?`,
		monitorID, from.UnixMilli(), to.UnixMilli())
	if err := row.Scan(&sum.Total, &sum.Failed, &avg, &peak, &avgAnswers); err != nil {
		return CheckSummary{}, fmt.Errorf("summarize checks: %w", err)
	}
	if avgAnswers {
		sum.AvgLatency = time.Duration(avg * float64(time.Millisecond))
		sum.MaxLatency = time.Duration(peak) * time.Millisecond
	}
	return sum, nil
}

// ListFailedChecks returns up to limit failed checks of a monitor in the
// window, newest first.
func (s *SQLite) ListFailedChecks(ctx context.Context, monitorID int64, from, to time.Time, limit int) ([]CheckResult, error) {
	rows, err := s.read.QueryContext(ctx,
		`SELECT `+resultColumns+`
		 FROM check_results WHERE monitor_id = ? AND checked_at >= ? AND checked_at < ? AND NOT up
		 ORDER BY checked_at DESC, id DESC LIMIT ?`,
		monitorID, from.UnixMilli(), to.UnixMilli(), limit)
	if err != nil {
		return nil, fmt.Errorf("list failed checks: %w", err)
	}
	return scanCheckResults(rows)
}

// FailedChecksEnds reads the failed checks of a monitor in the window,
// oldest first, in up to three indexed queries: their number, the oldest
// and, when they do not all fit, the newest. A failure stored meanwhile
// only moves the newest along, away from the oldest.
func (s *SQLite) FailedChecksEnds(ctx context.Context, monitorID int64, from, to time.Time, keep int) ([]CheckResult, int, error) {
	const where = ` FROM check_results WHERE monitor_id = ? AND checked_at >= ? AND checked_at < ? AND NOT up`
	const columns = resultColumns // with the diagnostics, which say why each one failed
	var total int
	if err := s.read.QueryRowContext(ctx, "SELECT COUNT(*)"+where, monitorID, from.UnixMilli(), to.UnixMilli()).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed checks: %w", err)
	}
	oldest, newest := Ends(total, keep)
	if oldest == 0 {
		return nil, total, nil
	}
	rows, err := s.read.QueryContext(ctx, "SELECT "+columns+where+" ORDER BY checked_at, id LIMIT ?",
		monitorID, from.UnixMilli(), to.UnixMilli(), oldest)
	if err != nil {
		return nil, 0, fmt.Errorf("failed checks: %w", err)
	}
	first, err := scanCheckResults(rows)
	if err != nil || newest == 0 {
		return first, total, err
	}
	rows, err = s.read.QueryContext(ctx,
		"SELECT "+columns+" FROM (SELECT id, "+columns+where+" ORDER BY checked_at DESC, id DESC LIMIT ?) ORDER BY checked_at, id",
		monitorID, from.UnixMilli(), to.UnixMilli(), newest)
	if err != nil {
		return nil, 0, fmt.Errorf("failed checks: %w", err)
	}
	last, err := scanCheckResults(rows)
	if err != nil {
		return nil, 0, err
	}
	return append(first, last...), total, nil
}

// ListIncidentsBetween returns the incidents of a monitor that overlap the
// window: begun before it ends and not resolved before it starts. Newest
// first, at most limit.
func (s *SQLite) ListIncidentsBetween(ctx context.Context, monitorID int64, from, to time.Time, limit int) ([]Incident, error) {
	rows, err := s.read.QueryContext(ctx,
		"SELECT "+incidentColumns+` FROM incidents i JOIN monitors m ON m.id = i.monitor_id
		 WHERE i.monitor_id = ?1 AND i.started_at < ?3 AND (i.resolved_at IS NULL OR i.resolved_at > ?2)
		 ORDER BY i.started_at DESC, i.id DESC LIMIT ?4`,
		monitorID, from.UnixMilli(), to.UnixMilli(), limit)
	if err != nil {
		return nil, fmt.Errorf("list incidents between: %w", err)
	}
	defer rows.Close()
	var out []Incident
	for rows.Next() {
		i, err := scanIncident(rows)
		if err != nil {
			return nil, fmt.Errorf("list incidents between: %w", err)
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list incidents between: %w", err)
	}
	return out, nil
}

// GridCell counts a monitor's checks in one bucket of time.
type GridCell struct {
	Start      time.Time
	Checks     int
	Failed     int
	AvgLatency time.Duration // over the successful checks; zero without any
	// Error is the most frequent error of the failed checks; empty when
	// none failed or they said nothing.
	Error string
}

// GridError is one error text and how often and when it was seen.
type GridError struct {
	Error       string
	Count       int
	First, Last time.Time
}

// CheckGrid counts the checks of a monitor from `from` to `to` in buckets
// aligned to the epoch, leaving empty buckets out, and names the most
// frequent error of each.
func (s *SQLite) CheckGrid(ctx context.Context, monitorID int64, from, to time.Time, bucket time.Duration) ([]GridCell, error) {
	ms := bucket.Milliseconds()
	if ms <= 0 {
		return nil, fmt.Errorf("check grid: bucket %s is too small", bucket)
	}
	rows, err := s.read.QueryContext(ctx,
		`SELECT (checked_at / ?1) * ?1 AS b, COUNT(*), SUM(CASE WHEN up THEN 0 ELSE 1 END),
		        COALESCE(AVG(CASE WHEN up THEN latency_ms END), 0)
		 FROM check_results WHERE monitor_id = ?2 AND checked_at >= ?3 AND checked_at < ?4
		 GROUP BY b ORDER BY b`, ms, monitorID, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("check grid: %w", err)
	}
	defer rows.Close()
	var cells []GridCell
	index := make(map[int64]int)
	for rows.Next() {
		var (
			start int64
			c     GridCell
			avg   float64
		)
		if err := rows.Scan(&start, &c.Checks, &c.Failed, &avg); err != nil {
			return nil, fmt.Errorf("check grid: %w", err)
		}
		c.Start = time.UnixMilli(start).UTC()
		c.AvgLatency = time.Duration(avg * float64(time.Millisecond))
		index[start] = len(cells)
		cells = append(cells, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("check grid: %w", err)
	}

	// The most frequent error per bucket: the first row of each bucket.
	errs, err := s.read.QueryContext(ctx,
		`SELECT (checked_at / ?1) * ?1 AS b, COALESCE(error, ''), COUNT(*) AS n
		 FROM check_results WHERE monitor_id = ?2 AND checked_at >= ?3 AND checked_at < ?4 AND NOT up
		 GROUP BY b, error ORDER BY b, n DESC`, ms, monitorID, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("check grid: %w", err)
	}
	defer errs.Close()
	seen := make(map[int64]bool)
	for errs.Next() {
		var (
			start int64
			text  string
			n     int
		)
		if err := errs.Scan(&start, &text, &n); err != nil {
			return nil, fmt.Errorf("check grid: %w", err)
		}
		if i, ok := index[start]; ok && !seen[start] {
			cells[i].Error, seen[start] = text, true
		}
	}
	return cells, errs.Err()
}

// TopErrors returns the most frequent errors of a monitor's failed checks
// in the window, most frequent first, at most limit.
func (s *SQLite) TopErrors(ctx context.Context, monitorID int64, from, to time.Time, limit int) ([]GridError, error) {
	rows, err := s.read.QueryContext(ctx,
		`SELECT COALESCE(error, ''), COUNT(*) AS n, MIN(checked_at), MAX(checked_at)
		 FROM check_results WHERE monitor_id = ? AND checked_at >= ? AND checked_at < ? AND NOT up
		 GROUP BY error ORDER BY n DESC, MAX(checked_at) DESC LIMIT ?`,
		monitorID, from.UnixMilli(), to.UnixMilli(), limit)
	if err != nil {
		return nil, fmt.Errorf("top errors: %w", err)
	}
	defer rows.Close()
	var out []GridError
	for rows.Next() {
		var (
			e           GridError
			first, last int64
		)
		if err := rows.Scan(&e.Error, &e.Count, &first, &last); err != nil {
			return nil, fmt.Errorf("top errors: %w", err)
		}
		e.First, e.Last = time.UnixMilli(first).UTC(), time.UnixMilli(last).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}
