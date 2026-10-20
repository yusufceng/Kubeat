package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

func encoded(t *testing.T, d *Diagnostics) string {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A failed result keeps its diagnostics, and every reader of single
// results gets them back; a successful one keeps none, whatever it carries.
func TestDiagnosticsAreKeptForFailedResults(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	m, err := s.CreateMonitor(ctx, newMonitor("api"))
	if err != nil {
		t.Fatal(err)
	}
	first := 1.25
	diagnostics := &Diagnostics{
		Explanation: &Explanation{Code: "dns_service_not_found", Params: map[string]string{"service": "api", "namespace": "shop"}},
		Timing:      &Timing{Connect: &first, Total: 3.5},
		RemoteAddr:  "10.128.2.15:8080",
		TLS:         &TLSDetails{Version: "TLS 1.3", Subject: "api.shop.svc", SANs: []string{"api.shop.svc"}, NotAfter: time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)},
		StatusLine:  "HTTP/1.1 503 Service Unavailable",
		Headers:     map[string]string{"server": "envoy"},
		Pod:         "api-7d9f-a",
	}
	base := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	batch := []CheckResult{
		{MonitorID: m.ID, CheckedAt: base, Up: true, Latency: time.Millisecond, Diagnostics: diagnostics},
		{MonitorID: m.ID, CheckedAt: base.Add(time.Second), Up: false, Error: "refused"},
		{MonitorID: m.ID, CheckedAt: base.Add(2 * time.Second), Up: false, Error: "unexpected status 503", StatusCode: 503, Diagnostics: diagnostics},
	}
	if err := s.InsertCheckResults(ctx, batch); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListCheckResults(ctx, m.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Diagnostics == nil || encoded(t, got[0].Diagnostics) != encoded(t, diagnostics) {
		t.Fatalf("newest = %+v", got[0])
	}
	if got[1].Diagnostics != nil || got[2].Diagnostics != nil {
		t.Errorf("a result without diagnostics has some: %+v, %+v", got[1].Diagnostics, got[2].Diagnostics)
	}
	var stored sql.NullString
	if err := s.read.QueryRowContext(ctx, "SELECT diagnostics FROM check_results WHERE up").Scan(&stored); err != nil || stored.Valid {
		t.Errorf("the successful result stored %v (%v)", stored, err)
	}

	failed, err := s.ListFailedChecks(ctx, m.ID, base, base.Add(time.Minute), 10)
	if err != nil || len(failed) != 2 || failed[0].Diagnostics == nil || failed[0].Diagnostics.Pod != "api-7d9f-a" {
		t.Errorf("failed checks = %+v, %v", failed, err)
	}
	// So do the failed checks on an incident's timeline, oldest first.
	ends, total, err := s.FailedChecksEnds(ctx, m.ID, base, base.Add(time.Minute), 10)
	if err != nil || total != 2 || len(ends) != 2 || ends[1].Diagnostics == nil || ends[1].Diagnostics.Pod != "api-7d9f-a" {
		t.Errorf("failed checks for the timeline = %+v, %d, %v", ends, total, err)
	}
	st, err := s.GetMonitorStatus(ctx, m.ID, base)
	if err != nil || st.Last == nil || st.Last.Diagnostics == nil || st.Last.Diagnostics.Explanation.Params["namespace"] != "shop" {
		t.Errorf("last result = %+v, %v", st.Last, err)
	}
	// The histories of many monitors come without them.
	recent, err := s.RecentCheckResults(ctx, []int64{m.ID}, 10)
	if err != nil || len(recent[m.ID]) != 3 || recent[m.ID][0].Diagnostics != nil || recent[m.ID][0].Error != "unexpected status 503" {
		t.Errorf("recent = %+v, %v", recent[m.ID], err)
	}

	// Diagnostics that cannot be read do not take their result with them.
	if _, err := s.write.ExecContext(ctx, "UPDATE check_results SET diagnostics = '{\"timing\": \"later\"}' WHERE NOT up AND diagnostics IS NOT NULL"); err != nil {
		t.Fatal(err)
	}
	got, err = s.ListCheckResults(ctx, m.ID, 1)
	if err != nil || len(got) != 1 || got[0].Diagnostics != nil || got[0].Error != "unexpected status 503" {
		t.Errorf("unreadable diagnostics: %+v, %v", got, err)
	}

	// They go with their results.
	if n, err := s.DeleteCheckResultsBefore(ctx, base.Add(time.Hour)); err != nil || n != 3 {
		t.Errorf("retention deleted %d, %v", n, err)
	}
}
