package alert

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// leakySender fails the way a careless provider error would: with the
// channel's address and secret in the text.
type leakySender struct{}

func (leakySender) Send(_ context.Context, ch store.Channel, _ Notification) error {
	return fmt.Errorf("posting to %s with key %s: 403 Forbidden", ch.Target, ch.Secret)
}

func (f *fixture) deliveries(incidentID int64) []store.Delivery {
	f.t.Helper()
	log, _, err := f.db.DeliveriesEnds(context.Background(), incidentID, 100)
	if err != nil {
		f.t.Fatal(err)
	}
	return log
}

// Every attempt is logged on the incident it was about, after its retries:
// what was reported, when that happened, and whether the channel took it.
func TestDeliveryLogRecordsEachAttempt(t *testing.T) {
	f := newFixture(t, 2, 1) // "ops" is a webhook
	first := f.check(false)
	second := f.check(false)
	incident := f.incidents()[0]
	if !incident.StartedAt.Equal(first.CheckedAt) || !incident.OpenedAt.Equal(second.CheckedAt) {
		t.Fatalf("incident started %v and opened %v, want %v and %v", incident.StartedAt, incident.OpenedAt, first.CheckedAt, second.CheckedAt)
	}
	f.flush()

	log := f.deliveries(incident.ID)
	if len(log) != 1 {
		t.Fatalf("log = %+v, want one entry", log)
	}
	down := log[0]
	if down.Event != "down" || !down.EventAt.Equal(second.CheckedAt) || down.ChannelName != "ops" || down.ChannelType != store.ChannelWebhook ||
		down.MonitorID != f.monitor.ID || !down.Delivered || down.Error != "" || down.GroupSize != 1 || down.At.IsZero() {
		t.Errorf("down entry = %+v", down)
	}

	// All three tries of the recovery fail: one entry, not delivered.
	f.sender.failures = 3
	recovered := f.check(true)
	f.flush()
	log = f.deliveries(incident.ID)
	if len(log) != 2 {
		t.Fatalf("log = %+v, want two entries", log)
	}
	if up := log[1]; up.Event != "up" || !up.EventAt.Equal(recovered.CheckedAt) || up.Delivered || up.Error != "boom" {
		t.Errorf("up entry = %+v", up)
	}
}

// The error is redacted before it is written: the entry outlives the
// channel's address and secret.
func TestDeliveryLogIsRedacted(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.engine.sender = leakySender{}
	ctx := context.Background()
	if _, err := f.db.CreateChannel(ctx, store.Channel{Name: "gotify", Type: store.ChannelGotify, Target: "https://push.example.com/s3cret-path",
		Secret: "Agotify-t0ken", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	f.check(false)
	f.flush()

	log := f.deliveries(f.incidents()[0].ID)
	if len(log) != 2 {
		t.Fatalf("log = %+v, want an entry per channel", log)
	}
	for _, d := range log {
		if d.Delivered || d.Error == "" {
			t.Errorf("%s: entry = %+v", d.ChannelName, d)
		}
	}
	if got := log[1].Error; got != "posting to https://push.example.com/… with key ********: 403 Forbidden" {
		t.Errorf("redacted error = %q", got)
	}
}

// A grouped message is an entry on each of its incidents; the channels
// software reads get them one by one, and log them so.
func TestDeliveryLogOfAGroupedMessage(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops" is a webhook
	f.engine.opts.GroupWait = 50 * time.Millisecond
	ctx := context.Background()
	if _, err := f.db.CreateChannel(ctx, store.Channel{Name: "chat", Type: store.ChannelSlack, Target: "http://unused", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	var notes []Notification
	for _, name := range []string{"cart", "orders"} {
		m, err := f.db.CreateMonitor(ctx, store.Monitor{Name: name, Type: store.MonitorTCP, Target: name + ":80", Namespace: "shop",
			Interval: 30 * time.Second, Timeout: 5 * time.Second, FailureThreshold: 1, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		incident, err := f.db.OpenIncident(ctx, store.Incident{MonitorID: m.ID, StartedAt: f.clock, OpenedAt: f.clock, Cause: "refused"})
		if err != nil {
			t.Fatal(err)
		}
		notes = append(notes, Notification{Event: EventDown, Monitor: m, Incident: incident, At: f.clock})
	}
	for _, n := range notes {
		f.engine.enqueue(n)
	}
	f.flush()

	for _, n := range notes {
		log := f.deliveries(n.Incident.ID)
		got := map[string]store.Delivery{}
		for _, d := range log {
			got[d.ChannelName] = d
		}
		if len(log) != 2 || got["ops"].GroupSize != 1 || got["chat"].GroupSize != 2 || !got["chat"].Delivered ||
			got["chat"].MonitorID != n.Monitor.ID || !got["chat"].EventAt.Equal(f.clock) {
			t.Errorf("%s: log = %+v", n.Monitor.Name, log)
		}
	}
}

func TestDeliveryLogEntries(t *testing.T) {
	ch := store.Channel{ID: 3, Name: "pager", Type: store.ChannelPagerDuty, Secret: "routing-key"}
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	// A test is about no incident.
	if got := deliveryLog(ch, Notification{Event: EventTest}, at, ""); len(got) != 0 {
		t.Errorf("a test was logged: %+v", got)
	}
	// A group's members are logged, those without an incident left out;
	// a member without a moment of its own takes the attempt's.
	group := Notification{Event: EventReminder, Group: []Notification{
		{Event: EventReminder, Monitor: store.Monitor{ID: 1}, Incident: store.Incident{ID: 10}, At: at.Add(-time.Minute)},
		{Event: EventReminder, Monitor: store.Monitor{ID: 2}, Incident: store.Incident{ID: 11}},
		{Event: EventReminder, Monitor: store.Monitor{ID: 3}},
	}}
	got := deliveryLog(ch, group, at, "PagerDuty answered 400: bad key routing-key")
	if len(got) != 2 || got[0].IncidentID != 10 || got[1].IncidentID != 11 || got[0].GroupSize != 3 || got[0].Event != "reminder" ||
		!got[0].EventAt.Equal(at.Add(-time.Minute)) || !got[1].EventAt.Equal(at) || got[0].Delivered || got[0].Error != "PagerDuty answered 400: bad key ********" {
		t.Errorf("entries = %+v", got)
	}
}
