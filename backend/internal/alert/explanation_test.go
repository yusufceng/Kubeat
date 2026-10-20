package alert

import (
	"context"
	"testing"

	"github.com/yusufceng/Kubeat/backend/internal/explain"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// lastFact is the value of a notification's last fact, its cause.
func lastFact(n Notification) Fact {
	facts := Facts(n)
	return facts[len(facts)-1]
}

// The cause of an outage that just opened says what it most likely means,
// in the install's language; other events and causes nothing recognized
// keep the error alone.
func TestCauseCarriesTheExplanation(t *testing.T) {
	n := sampleDown()
	n.Explanation = &store.Explanation{Code: explain.RouteNoEndpoints}
	want := "unexpected status 503 — " + explain.Text(*n.Explanation, explain.Turkish)
	if got := lastFact(n); got.Label != "Neden" || got.Value != want {
		t.Errorf("Turkish: %+v", got)
	}
	n.Language = LanguageEnglish
	want = "unexpected status 503 — The OpenShift router has nowhere to send this request: no Route matches its host and path, or the Route's Service has no ready pods."
	if got := lastFact(n); got.Label != "Cause" || got.Value != want {
		t.Errorf("English: %+v", got)
	}
	// The one-line text stays as short as it was.
	if text := Text(n); text != "🔴 ödeme-api is down (https://pay.example.com/healthz). 3 checks in a row failed. Last error: unexpected status 503" {
		t.Errorf("text = %q", text)
	}

	reminder := n
	reminder.Event = EventReminder
	if got := lastFact(reminder); got.Value != "unexpected status 503" {
		t.Errorf("reminder: %+v", got)
	}
	unknown := sampleDown()
	unknown.Explanation = &store.Explanation{Code: "from_a_later_version"}
	if got := lastFact(unknown); got.Value != "unexpected status 503" {
		t.Errorf("an unknown code: %+v", got)
	}
	if got := lastFact(sampleDown()); got.Value != "unexpected status 503" {
		t.Errorf("without an explanation: %+v", got)
	}
}

// The engine hands the newest failure's explanation, the one whose error
// became the cause, to the notification of the outage.
func TestDownNotificationIsExplained(t *testing.T) {
	f := newFixture(t, 2, 1)
	ctx := context.Background()
	fail := func(code string) {
		f.clock = f.clock.Add(f.monitor.Interval)
		r := store.CheckResult{MonitorID: f.monitor.ID, CheckedAt: f.clock, Up: false, Error: "connection refused",
			Diagnostics: &store.Diagnostics{Explanation: &store.Explanation{Code: code}}}
		if err := f.db.InsertCheckResults(ctx, []store.CheckResult{r}); err != nil {
			t.Fatal(err)
		}
		f.engine.Evaluate(ctx, []store.CheckResult{r})
	}
	fail(explain.ConnectTimeout)
	fail(explain.ConnectionRefused)
	f.flush()
	if len(f.sender.sent) != 1 {
		t.Fatalf("sent = %+v", f.sender.sent)
	}
	down := f.sender.sent[0]
	if down.Event != EventDown || down.Explanation == nil || down.Explanation.Code != explain.ConnectionRefused {
		t.Errorf("down = %+v", down)
	}
	if got := lastFact(down); got.Value != "connection refused — "+explain.Text(store.Explanation{Code: explain.ConnectionRefused}, explain.Turkish) {
		t.Errorf("cause = %+v", got)
	}
}
