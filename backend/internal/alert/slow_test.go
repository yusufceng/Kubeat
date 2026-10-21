package alert

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// checkTook stores a result with the given response time and evaluates it.
func (f *fixture) checkTook(up bool, took time.Duration) store.CheckResult {
	f.t.Helper()
	f.clock = f.clock.Add(30 * time.Second)
	r := store.CheckResult{MonitorID: f.monitor.ID, CheckedAt: f.clock, Up: up, Latency: took}
	if !up {
		r.Error = "timeout"
	}
	if err := f.db.InsertCheckResults(context.Background(), []store.CheckResult{r}); err != nil {
		f.t.Fatal(err)
	}
	f.engine.Evaluate(context.Background(), []store.CheckResult{r})
	return r
}

func (f *fixture) slowSince() time.Time {
	f.t.Helper()
	m, err := f.db.GetMonitor(context.Background(), f.monitor.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return m.SlowSince
}

func (f *fixture) setSlowAbove(threshold time.Duration) {
	f.t.Helper()
	m, err := f.db.GetMonitor(context.Background(), f.monitor.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	m.SlowAbove = threshold
	if f.monitor, err = f.db.UpdateMonitor(context.Background(), m); err != nil {
		f.t.Fatal(err)
	}
}

func TestSlowResponse(t *testing.T) {
	f := newFixture(t, 2, 1)
	f.setSlowAbove(500 * time.Millisecond)

	f.checkTook(true, 800*time.Millisecond) // one of two
	if !f.slowSince().IsZero() {
		t.Fatal("slow after one slow check with a threshold of two")
	}
	first := f.checkTook(true, 900*time.Millisecond)
	since := f.slowSince()
	if !since.Equal(first.CheckedAt.Add(-30 * time.Second)) {
		t.Fatalf("SlowSince = %v, want the first slow check %v", since, first.CheckedAt.Add(-30*time.Second))
	}
	f.checkTook(true, 950*time.Millisecond) // still slow: nothing new
	f.checkTook(false, 2*time.Second)       // a failure changes nothing about slowness
	if !f.slowSince().Equal(since) {
		t.Errorf("SlowSince after more checks = %v, want unchanged %v", f.slowSince(), since)
	}
	if got := f.incidents(); len(got) != 0 {
		t.Errorf("a slow monitor opened an incident: %+v", got)
	}
	fast := f.checkTook(true, 100*time.Millisecond)
	if !f.slowSince().IsZero() {
		t.Error("still slow after a check within the threshold")
	}

	f.flush()
	if events := f.sender.events(); len(events) != 2 || events[0] != EventSlow || events[1] != EventSlowResolved {
		t.Fatalf("notifications = %v, want [slow slow_resolved]", events)
	}
	slow, resolved := f.sender.sent[0], f.sender.sent[1]
	if text := Text(slow); !strings.Contains(text, "yavaş yanıt veriyor") || !strings.Contains(text, "Art arda 2 kontrol") ||
		!strings.Contains(text, "500 ms") || !strings.Contains(text, "sonuncusu 900 ms") {
		t.Errorf("slow text = %q", text)
	}
	if slow.Incident.ID != 0 || !slow.Since.Equal(since) || !slow.At.Equal(first.CheckedAt) || slow.Latency != 900*time.Millisecond {
		t.Errorf("slow notification = %+v", slow)
	}
	if text := Text(resolved); !strings.Contains(text, "yeniden zamanında yanıt veriyor") || !strings.Contains(text, "100 ms") || !strings.Contains(text, "2 dk") {
		t.Errorf("resolved text = %q (slow for 2 minutes)", text)
	}
	if !resolved.At.Equal(fast.CheckedAt) || !resolved.Since.Equal(since) {
		t.Errorf("resolved notification = %+v", resolved)
	}
	// About no incident: the delivery log keeps nothing.
	if entries := deliveryLog(store.Channel{ID: 1}, slow, f.clock, ""); len(entries) != 0 {
		t.Errorf("a slow notification was logged: %+v", entries)
	}
	if got := Subject(slow); got != "[Kubeat] api yavaş yanıt veriyor" {
		t.Errorf("subject = %q", got)
	}
	facts := Facts(resolved)
	if len(facts) != 6 || facts[2].Label != "Başlangıç" || facts[3].Label != "Bitiş" || facts[4].Value != "500 ms" || facts[5].Value != "100 ms" {
		t.Errorf("facts = %+v", facts)
	}
}

func TestSlowResponseOffAndInMaintenance(t *testing.T) {
	f := newFixture(t, 1, 1) // the threshold is off
	for i := 0; i < 3; i++ {
		f.checkTook(true, 5*time.Second)
	}
	if !f.slowSince().IsZero() {
		t.Fatal("slow without a threshold")
	}

	f.setSlowAbove(time.Second)
	f.maintain(time.Minute)
	f.checkTook(true, 5*time.Second) // in the window: not raised
	if !f.slowSince().IsZero() {
		t.Error("turned slow during maintenance")
	}
	f.checkTook(true, 5*time.Second) // the window is over
	if f.slowSince().IsZero() {
		t.Fatal("not slow after the window")
	}
	// Slow when the window begins, in time again inside it: the story
	// still gets its ending.
	f.maintain(time.Minute)
	f.checkTook(true, 100*time.Millisecond)
	if !f.slowSince().IsZero() {
		t.Error("stayed slow after a fast check in maintenance")
	}
	f.flush()
	if events := f.sender.events(); len(events) != 2 || events[0] != EventSlow || events[1] != EventSlowResolved {
		t.Errorf("notifications = %v, want [slow slow_resolved]", events)
	}

	// Turning the threshold off, or pausing the monitor, ends the state
	// without a notification.
	f.checkTook(true, 5*time.Second)
	if f.slowSince().IsZero() {
		t.Fatal("not slow again")
	}
	f.setSlowAbove(0)
	if !f.slowSince().IsZero() {
		t.Error("still slow with the threshold off")
	}
	f.setSlowAbove(time.Second)
	f.checkTook(true, 5*time.Second)
	paused, _ := f.db.GetMonitor(context.Background(), f.monitor.ID)
	paused.Enabled = false
	if _, err := f.db.UpdateMonitor(context.Background(), paused); err != nil {
		t.Fatal(err)
	}
	if !f.slowSince().IsZero() {
		t.Error("still slow while paused")
	}
	f.flush()
	// Slow twice more, never told to be in time again: the ends by
	// setting are silent.
	if events := f.sender.events(); len(events) != 4 || events[2] != EventSlow || events[3] != EventSlow {
		t.Errorf("notifications = %v, want [slow slow_resolved slow slow]", events)
	}
}

func TestSlowAndTestTextsInEnglish(t *testing.T) {
	at := time.Date(2026, 10, 13, 3, 0, 0, 0, time.UTC)
	m := store.Monitor{ID: 4, Name: "api", Target: "http://api/", Namespace: "shop", FailureThreshold: 3}
	slow := Notification{Event: EventSlow, Monitor: m, At: at, Since: at.Add(-time.Minute), Threshold: 500 * time.Millisecond, Latency: 1200 * time.Millisecond, PublicURL: "https://kubeat.example"}
	if got := Text(slow); got != "🟠 api is slow (http://api/). 3 checks in a row took longer than 500 ms; the last one took 1200 ms" {
		t.Errorf("slow = %q", got)
	}
	if got := URL(slow); got != "https://kubeat.example/monitors/4?from=2026-10-13T02%3A59%3A00Z" {
		t.Errorf("slow url = %q", got)
	}
	resolved := slow
	resolved.Event, resolved.Latency, resolved.At = EventSlowResolved, 200*time.Millisecond, at.Add(90*time.Second)
	if got := Text(resolved); got != "🟢 api responds in time again (http://api/): 200 ms. It was slow for 2m 30s" {
		t.Errorf("resolved = %q", got)
	}
	if got := URL(resolved); got != "https://kubeat.example/monitors/4?from=2026-10-13T02%3A59%3A00Z&to=2026-10-13T03%3A01%3A30Z" {
		t.Errorf("resolved url = %q", got)
	}
	group := Notification{Event: EventSlow, Monitor: m, Group: []Notification{slow, slow}}
	if got := Text(group); got != "🟠 shop: 2 monitors are slow: api (1200 ms), api (1200 ms)" {
		t.Errorf("group = %q", got)
	}
	if got := Subject(group); got != "[Kubeat] shop: 2 monitors are slow" {
		t.Errorf("group subject = %q", got)
	}
	if Color(slow) != colorWarning || Emoji(slow) != "🟠" || Color(resolved) != colorUp {
		t.Error("slow colours")
	}
	body := newWebhookBody(slow)
	if body.Incident != nil || body.Slow == nil || body.Slow.ThresholdMS != 500 || body.Slow.LatencyMS != 1200 || body.Monitor.Name != "api" {
		t.Errorf("webhook body = %+v", body)
	}

	// A test from a monitor names it.
	test := Notification{Event: EventTest, Monitor: m}
	if got := Text(test); got != "Kubeat test notification: the alarms of api (http://api/) reach this channel." {
		t.Errorf("test = %q", got)
	}
	if got := Subject(test); got != "[Kubeat] Test notification: api" {
		t.Errorf("test subject = %q", got)
	}
	if facts := Facts(test); len(facts) != 3 || facts[0].Value != "api" || facts[2].Value != "shop" {
		t.Errorf("test facts = %+v", facts)
	}
	if body := newWebhookBody(test); body.Monitor == nil || body.Monitor.ID != 4 || body.Incident != nil {
		t.Errorf("test body = %+v", body)
	}
	if body := newWebhookBody(Notification{Event: EventTest}); body.Monitor != nil {
		t.Errorf("plain test body = %+v", body)
	}
	kinds := map[Event]string{EventSlow: "slow", EventSlowResolved: "slow_resolved"}
	for e, want := range kinds {
		if e.Kind() != want || e.Outage() {
			t.Errorf("%s: kind %q, outage %v", e, e.Kind(), e.Outage())
		}
	}
}
