package alert

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func TestGroupedDelivery(t *testing.T) {
	f := newFixture(t, 1, 1) // "ops" is a webhook with no scope
	f.engine.opts.GroupWait = 50 * time.Millisecond
	ctx := context.Background()
	for _, ch := range []store.Channel{
		{Name: "chat", Type: store.ChannelSlack, Target: "http://unused"},
		{Name: "shop-mail", Type: store.ChannelEmail, Target: "shop@example.com", Namespaces: []string{"shop"}},
		{Name: "pager", Type: store.ChannelPagerDuty, Secret: "key", Tags: []string{"pager"}, Events: []string{"down", "up"}},
		{Name: "quiet", Type: store.ChannelSlack, Target: "http://unused", Events: []string{"cert"}},
	} {
		ch.Enabled = true
		if _, err := f.db.CreateChannel(ctx, ch); err != nil {
			t.Fatal(err)
		}
	}
	monitor := func(name, namespace string, tags ...string) store.Monitor {
		return store.Monitor{Name: name, Namespace: namespace, Tags: tags, Target: "http://" + name}
	}
	incident := store.Incident{ID: 1, StartedAt: f.clock, Cause: "refused"}

	// Three outages in shop within the wait, one elsewhere.
	for _, n := range []Notification{
		{Event: EventDown, Monitor: monitor("api", "shop", "pager"), Incident: incident},
		{Event: EventDown, Monitor: monitor("db", "shop"), Incident: incident},
		{Event: EventDown, Monitor: monitor("cache", "shop"), Incident: incident},
		{Event: EventDown, Monitor: monitor("other", "billing"), Incident: incident},
	} {
		f.engine.enqueue(n)
	}
	f.flush() // shutdown flushes the groups at once

	f.sender.mu.Lock()
	defer f.sender.mu.Unlock()
	got := make(map[string][]string) // channel -> what it was sent
	for i, n := range f.sender.sent {
		what := n.Monitor.Name
		if len(n.Group) > 1 {
			names := make([]string, 0, len(n.Group))
			for _, g := range n.Group {
				names = append(names, g.Monitor.Name)
			}
			what = "group[" + strings.Join(names, " ") + "]"
		}
		got[f.sender.channels[i]] = append(got[f.sender.channels[i]], what)
	}
	want := map[string]string{
		"ops":       "api db cache other",        // a webhook gets every one on its own
		"chat":      "group[api db cache] other", // people get one message per namespace
		"shop-mail": "group[api db cache]",       // scoped to shop
		"pager":     "api",                       // only the tagged monitor, on its own
		"quiet":     "",                          // only certificates
	}
	for ch, w := range want {
		if g := strings.Join(got[ch], " "); g != w {
			t.Errorf("%s got %q, want %q", ch, g, w)
		}
	}
}

func TestGroupWaitZeroSendsAtOnce(t *testing.T) {
	f := newFixture(t, 1, 1)
	f.engine.enqueue(Notification{Event: EventDown, Monitor: store.Monitor{Name: "a", Namespace: "shop"}})
	f.engine.enqueue(Notification{Event: EventDown, Monitor: store.Monitor{Name: "b", Namespace: "shop"}})
	f.flush()
	if events := f.sender.events(); len(events) != 2 {
		t.Errorf("sent %v, want two separate notifications", events)
	}
}

func TestGroupedTexts(t *testing.T) {
	at := time.Date(2026, 10, 13, 3, 0, 0, 0, time.UTC)
	down := func(name, cause string) Notification {
		return Notification{Event: EventDown, Monitor: store.Monitor{Name: name, Namespace: "shop"}, Incident: store.Incident{StartedAt: at, Cause: cause}}
	}
	n := Notification{Event: EventDown, Language: LanguageTurkish, Monitor: store.Monitor{Name: "api", Namespace: "shop"}, Group: []Notification{down("api", "refused"), down("db", "timeout")}}
	if got := Text(n); got != "🔴 shop: 2 monitor erişilemiyor: api (refused), db (timeout)" {
		t.Errorf("text = %q", got)
	}
	if got := Subject(n); got != "[Kubeat] shop: 2 monitor erişilemiyor" {
		t.Errorf("subject = %q", got)
	}
	if facts := Facts(n); len(facts) != 3 || facts[0].Label != "Namespace" || facts[1].Label != "api" || facts[2].Value != "timeout" {
		t.Errorf("facts = %+v", facts)
	}

	up := Notification{Event: EventUp, Language: LanguageTurkish, Monitor: store.Monitor{Name: "a"}, Group: []Notification{
		{Event: EventUp, Monitor: store.Monitor{Name: "a"}, Incident: store.Incident{StartedAt: at, ResolvedAt: at.Add(90 * time.Second)}},
		{Event: EventUp, Monitor: store.Monitor{Name: "b"}, Incident: store.Incident{StartedAt: at, ResolvedAt: at.Add(5 * time.Minute)}},
	}}
	if got := Text(up); got != "🟢 2 monitor tekrar çalışıyor: a (1m30s), b (5m0s)" {
		t.Errorf("up text = %q", got)
	}

	// Long groups are cut short.
	var many []Notification
	for i := 0; i < 13; i++ {
		many = append(many, down(string(rune('a'+i)), "x"))
	}
	if got := Text(Notification{Event: EventDown, Language: LanguageTurkish, Monitor: store.Monitor{Namespace: "shop"}, Group: many}); !strings.HasSuffix(got, ", ve 3 monitor daha") {
		t.Errorf("long group text = %q", got)
	}
}

func TestChannelScopeAndEvents(t *testing.T) {
	m := store.Monitor{Namespace: "shop", Tags: []string{"prod", "pager"}}
	for _, tt := range []struct {
		ch   store.Channel
		want bool
	}{
		{store.Channel{}, true},
		{store.Channel{Namespaces: []string{"shop"}}, true},
		{store.Channel{Namespaces: []string{"billing"}}, false},
		{store.Channel{Tags: []string{"pager"}}, true},
		{store.Channel{Tags: []string{"dev"}}, false},
		{store.Channel{Namespaces: []string{"shop"}, Tags: []string{"dev"}}, false},
	} {
		if got := tt.ch.Covers(m); got != tt.want {
			t.Errorf("Covers with %+v = %v, want %v", tt.ch, got, tt.want)
		}
	}
	ch := store.Channel{Events: []string{"down", "cert"}}
	for event, want := range map[Event]bool{EventDown: true, EventUp: false, EventReminder: false, EventCertExpiring: true, EventCertRenewed: true, EventTest: true} {
		if got := ch.Wants(event.Kind()); got != want {
			t.Errorf("Wants(%s) = %v, want %v", event, got, want)
		}
	}
}

func TestEnglishTexts(t *testing.T) {
	at := time.Date(2026, 10, 13, 3, 0, 0, 0, time.UTC)
	n := Notification{Event: EventDown, Monitor: store.Monitor{Name: "api", Target: "http://api/", FailureThreshold: 2},
		Incident: store.Incident{StartedAt: at, Cause: "refused"}}
	if got := Text(n); got != "🔴 api is down (http://api/). 2 checks in a row failed. Last error: refused" {
		t.Errorf("english by default: %q", got)
	}
	if got := Subject(n); got != "[Kubeat] api is down" {
		t.Errorf("subject = %q", got)
	}
	if facts := Facts(n); facts[1].Label != "Target" || facts[len(facts)-1].Label != "Cause" {
		t.Errorf("facts = %+v", facts)
	}
	grouped := Notification{Event: EventDown, Monitor: store.Monitor{Namespace: "shop"}, Group: []Notification{n, n}}
	if got := Text(grouped); got != "🔴 shop: 2 monitors are down: api (refused), api (refused)" {
		t.Errorf("grouped = %q", got)
	}
	if got := Text(Notification{Event: EventTest, Language: "xx"}); got != "Kubeat test notification: this channel works." {
		t.Errorf("unknown language falls back to English: %q", got)
	}
}
