package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (s *SQLite) Report(ctx context.Context, from, to time.Time) ([]MonitorReport, error) {
	monitors, err := s.ListMonitors(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*MonitorReport, len(monitors))
	out := make([]MonitorReport, len(monitors))
	for i, m := range monitors {
		out[i].Monitor = m
		byID[m.ID] = &out[i]
	}

	rows, err := s.read.QueryContext(ctx,
		`SELECT monitor_id, SUM(checks), SUM(up_checks), SUM(up_latency_sum_ms)
		 FROM check_rollups WHERE bucket_start >= ?1 AND bucket_start < ?2 GROUP BY monitor_id`,
		from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("report: %w", err)
	}
	for rows.Next() {
		var (
			id, checks, up, latencySum int64
		)
		if err := rows.Scan(&id, &checks, &up, &latencySum); err != nil {
			rows.Close()
			return nil, fmt.Errorf("report: %w", err)
		}
		r, ok := byID[id]
		if !ok {
			continue
		}
		r.Checks, r.UpChecks = int(checks), int(up)
		if up > 0 {
			r.AvgLatency = time.Duration(latencySum/up) * time.Millisecond
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("report: %w", err)
	}
	rows.Close()

	// Outages that touch the period, clipped to it.
	rows, err = s.read.QueryContext(ctx,
		`SELECT monitor_id, started_at, resolved_at FROM incidents
		 WHERE kind = 'down' AND started_at < ?2 AND (resolved_at IS NULL OR resolved_at > ?1)`,
		from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("report: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, started int64
			resolved    sql.NullInt64
		)
		if err := rows.Scan(&id, &started, &resolved); err != nil {
			return nil, fmt.Errorf("report: %w", err)
		}
		r, ok := byID[id]
		if !ok {
			continue
		}
		start := time.UnixMilli(started)
		end := to
		if resolved.Valid {
			end = time.UnixMilli(resolved.Int64)
		} else {
			r.OpenOutage = true
		}
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		if d := end.Sub(start); d > 0 {
			r.Downtime += d
			r.LongestOutage = max(r.LongestOutage, d)
		}
		r.Outages++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("report: %w", err)
	}
	return out, nil
}
