package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// The throttle's counters have every series from the start, at 0, so that
// increase() sees the first request the throttle could not key on its client
// and the first 429 of a password change or a status-page unlock; the labels
// come from closed sets and no other series can appear.
func TestThrottleCountersStartAtZero(t *testing.T) {
	got, err := New("test").Gather()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, reason := range []string{"hops_zero", "no_frontend_token", "untrusted_peer", "hops_too_high", "hops_too_low"} {
		for _, surface := range []string{"sign_in", "password_change", "status_page"} {
			want["kubeat_signin_source_degraded_total{reason="+reason+"}{surface="+surface+"}"] = true
		}
	}
	for _, kind := range []string{"source", "account"} {
		for _, surface := range []string{"password_change", "status_page"} {
			want["kubeat_throttled_total{kind="+kind+"}{surface="+surface+"}"] = true
		}
	}
	for key := range want {
		if value, ok := got[key]; !ok || value != 0 {
			t.Errorf("%s = %v (present: %v), want 0 at the start", key, value, ok)
		}
	}
	for key := range got {
		if (strings.HasPrefix(key, "kubeat_signin_source_degraded_total") || strings.HasPrefix(key, "kubeat_throttled_total")) && !want[key] {
			t.Errorf("unexpected series %s", key)
		}
	}
	if len(want) != 19 {
		t.Fatalf("the test lists %d series, want 15 + 4", len(want))
	}
}

// Each reason and surface counts on its own, and so does each kind of 429 of
// the two surfaces; the sign-in's 429s stay kubeat_signin_throttled_total's.
func TestThrottleCounters(t *testing.T) {
	m := New("test")
	m.SourceDegraded(DegradedHopsZero, SurfaceSignIn)
	m.SourceDegraded(DegradedHopsZero, SurfaceSignIn)
	m.SourceDegraded(DegradedHopsTooLow, SurfaceStatusPage)
	m.SourceDegraded(DegradedUntrustedPeer, SurfacePasswordChange)
	m.Throttled(SurfaceStatusPage, ThrottleSource)
	m.Throttled(SurfaceStatusPage, ThrottleSource)
	m.Throttled(SurfacePasswordChange, ThrottleAccount)

	got, err := m.Gather()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{
		"kubeat_signin_source_degraded_total{reason=hops_zero}{surface=sign_in}":              2,
		"kubeat_signin_source_degraded_total{reason=hops_too_low}{surface=status_page}":       1,
		"kubeat_signin_source_degraded_total{reason=untrusted_peer}{surface=password_change}": 1,
		"kubeat_signin_source_degraded_total{reason=hops_zero}{surface=status_page}":          0,
		"kubeat_throttled_total{kind=source}{surface=status_page}":                            2,
		"kubeat_throttled_total{kind=account}{surface=password_change}":                       1,
		"kubeat_throttled_total{kind=source}{surface=password_change}":                        0,
		"kubeat_signin_throttled_total{kind=source}":                                          0,
		"kubeat_signin_throttled_total{kind=account}":                                         0,
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %v, want %v", key, got[key], value)
		}
	}

	// The exposition carries the two counters with their help, and no
	// label but reason and surface, or surface and kind.
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, line := range []string{
		"# TYPE kubeat_signin_source_degraded_total counter",
		"# TYPE kubeat_throttled_total counter",
		`kubeat_signin_source_degraded_total{reason="hops_zero",surface="sign_in"} 2`,
		`kubeat_throttled_total{kind="source",surface="status_page"} 2`,
	} {
		if !strings.Contains(body, line) {
			t.Errorf("the exposition lacks %q", line)
		}
	}
}

// A nil *Metrics records nothing, the throttle's counters included.
func TestNilThrottleCounters(t *testing.T) {
	var m *Metrics
	m.SourceDegraded(DegradedHopsZero, SurfaceSignIn)
	m.Throttled(SurfaceStatusPage, ThrottleSource)
}
