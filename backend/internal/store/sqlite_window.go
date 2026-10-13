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
		`SELECT monitor_id, checked_at, up, latency_ms, status_code, error, detail, cert_expires_at
		 FROM check_results WHERE monitor_id = ? AND checked_at >= ? AND checked_at < ? AND NOT up
		 ORDER BY checked_at DESC, id DESC LIMIT ?`,
		monitorID, from.UnixMilli(), to.UnixMilli(), limit)
	if err != nil {
		return nil, fmt.Errorf("list failed checks: %w", err)
	}
	return scanCheckResults(rows)
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
