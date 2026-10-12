package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (s *SQLite) RollupCheckResults(ctx context.Context, from, to time.Time) error {
	if !from.Equal(from.Truncate(RollupBucket)) {
		return fmt.Errorf("rollup: from %s is not on an hour boundary", from.UTC().Format(time.RFC3339))
	}
	hour := RollupBucket.Milliseconds()
	_, err := s.write.ExecContext(ctx,
		`INSERT OR REPLACE INTO check_rollups
		     (monitor_id, bucket_start, checks, up_checks, up_latency_sum_ms, up_latency_max_ms)
		 SELECT monitor_id, (checked_at / ?1) * ?1, COUNT(*), SUM(up),
		        COALESCE(SUM(CASE WHEN up THEN latency_ms END), 0),
		        COALESCE(MAX(CASE WHEN up THEN latency_ms END), 0)
		 FROM check_results
		 WHERE checked_at >= ?2 AND checked_at < ?3
		 GROUP BY monitor_id, checked_at / ?1`,
		hour, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return fmt.Errorf("rollup: %w", err)
	}
	return nil
}

func (s *SQLite) DeleteRollupsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.write.ExecContext(ctx, "DELETE FROM check_rollups WHERE bucket_start < ?", cutoff.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("delete rollups: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete rollups: %w", err)
	}
	return n, nil
}

const seriesRawQuery = `
SELECT (checked_at / ?1) * ?1 AS bucket, COUNT(*), SUM(up),
       COALESCE(SUM(CASE WHEN up THEN latency_ms END), 0),
       COALESCE(MAX(CASE WHEN up THEN latency_ms END), 0)
FROM check_results
WHERE monitor_id = ?2 AND checked_at >= ?3 AND checked_at < ?4
GROUP BY bucket ORDER BY bucket`

const seriesRollupQuery = `
SELECT (bucket_start / ?1) * ?1 AS bucket, SUM(checks), SUM(up_checks),
       SUM(up_latency_sum_ms), MAX(up_latency_max_ms)
FROM check_rollups
WHERE monitor_id = ?2 AND bucket_start >= ?3 AND bucket_start < ?4
GROUP BY bucket ORDER BY bucket`

func (s *SQLite) Series(ctx context.Context, monitorID int64, source SeriesSource, from, to time.Time, bucket time.Duration) ([]SeriesPoint, error) {
	if bucket < time.Millisecond {
		return nil, fmt.Errorf("series: bucket %s is too small", bucket)
	}
	query := seriesRawQuery
	if source == SeriesRollup {
		if bucket%RollupBucket != 0 {
			return nil, fmt.Errorf("series: bucket %s is not a multiple of %s", bucket, RollupBucket)
		}
		query = seriesRollupQuery
	}

	rows, err := s.read.QueryContext(ctx, query, bucket.Milliseconds(), monitorID, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("series: %w", err)
	}
	defer rows.Close()

	var points []SeriesPoint
	for rows.Next() {
		var (
			p                   SeriesPoint
			start, sumMS, maxMS int64
			upChecks            sql.NullInt64
		)
		if err := rows.Scan(&start, &p.Checks, &upChecks, &sumMS, &maxMS); err != nil {
			return nil, fmt.Errorf("series: %w", err)
		}
		p.Start = time.UnixMilli(start).UTC()
		p.UpChecks = int(upChecks.Int64)
		if p.UpChecks > 0 {
			p.AvgLatency = time.Duration(float64(sumMS) / float64(p.UpChecks) * float64(time.Millisecond))
			p.MaxLatency = time.Duration(maxMS) * time.Millisecond
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("series: %w", err)
	}
	return points, nil
}
