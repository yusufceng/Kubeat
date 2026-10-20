package alert

import (
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func TestURLOfANotification(t *testing.T) {
	single := Notification{Event: EventDown, PublicURL: "https://kubeat.example/", Monitor: store.Monitor{ID: 7, Name: "api", Namespace: "shop"}}
	if got := URL(single); got != "https://kubeat.example/monitors/7" {
		t.Errorf("single = %q", got)
	}
	// A group is about a namespace, so it links to the namespace's page;
	// the monitors without one use the frontend's "_" slug.
	grouped := single
	grouped.Group = []Notification{single, single}
	if got := URL(grouped); got != "https://kubeat.example/namespaces/shop" {
		t.Errorf("grouped = %q", got)
	}
	grouped.Monitor.Namespace = ""
	if got := URL(grouped); got != "https://kubeat.example/namespaces/_" {
		t.Errorf("grouped without a namespace = %q", got)
	}
	grouped.Monitor.Namespace = "a/b c"
	if got := URL(grouped); got != "https://kubeat.example/namespaces/a%2Fb%20c" {
		t.Errorf("namespace needing escaping = %q", got)
	}
	// About an incident, the link is the incident's page, open or resolved.
	started := time.Date(2026, 10, 13, 11, 4, 1, 0, time.UTC)
	withIncident := single
	withIncident.Incident = store.Incident{ID: 3, StartedAt: started}
	if got := URL(withIncident); got != "https://kubeat.example/incidents/3" {
		t.Errorf("open incident = %q", got)
	}
	withIncident.Incident.ResolvedAt = started.Add(5 * time.Minute)
	if got := URL(withIncident); got != "https://kubeat.example/incidents/3" {
		t.Errorf("resolved incident = %q", got)
	}
	// A span without an incident to name lands on the monitor's chart,
	// with the span selected: an open one from its start, a resolved one
	// from start to end.
	span := single
	span.Incident = store.Incident{StartedAt: started}
	if got := URL(span); got != "https://kubeat.example/monitors/7?from=2026-10-13T11%3A04%3A01Z" {
		t.Errorf("open span = %q", got)
	}
	span.Incident.ResolvedAt = started.Add(5 * time.Minute)
	if got := URL(span); got != "https://kubeat.example/monitors/7?from=2026-10-13T11%3A04%3A01Z&to=2026-10-13T11%3A09%3A01Z" {
		t.Errorf("resolved span = %q", got)
	}

	// Without a public address there is no link rather than a broken one.
	single.PublicURL = ""
	if got := URL(single); got != "" {
		t.Errorf("without a public URL = %q", got)
	}
	// A test notification has no monitor; it points at the overview.
	test := Notification{Event: EventTest, PublicURL: "https://kubeat.example"}
	if got := URL(test); got != "https://kubeat.example" {
		t.Errorf("test = %q", got)
	}
}

func TestColors(t *testing.T) {
	for event, want := range map[Event]string{
		EventDown: colorDown, EventReminder: colorDown, EventUp: colorUp,
		EventCertRenewed: colorUp, EventCertExpiring: colorWarning, EventTest: colorNeutral,
	} {
		if got := Color(Notification{Event: event}); got != want {
			t.Errorf("Color(%s) = %s, want %s", event, got, want)
		}
	}
	if got := ColorInt(Notification{Event: EventDown}); got != 0xd7263d {
		t.Errorf("ColorInt = %#x", got)
	}
}
