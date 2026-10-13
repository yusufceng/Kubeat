package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

func TestChannelTagsAndEvents(t *testing.T) {
	srv, db := newTestAPI(t)
	base := srv.URL + "/api/v1/channels"

	var ch channelResponse
	code := do(t, http.MethodPost, base, `{"name":"pager","type":"slack","target":"https://hooks.example.com/x","tags":["Pager "," prod"],"events":["up","down","down"]}`, &ch)
	if code != http.StatusCreated || len(ch.Tags) != 2 || ch.Tags[0] != "Pager" || len(ch.Events) != 2 || ch.Events[0] != "down" || ch.Events[1] != "up" {
		t.Fatalf("create = %d %+v", code, ch)
	}
	var problem errorBody
	if code := do(t, http.MethodPost, base, `{"name":"bad","type":"slack","target":"https://hooks.example.com/x","events":["boom"]}`, &problem); code != http.StatusBadRequest {
		t.Errorf("bad event = %d %s, want 400", code, problem.Error)
	}
	if code := do(t, http.MethodPatch, base+"/"+itoa(ch.ID), `{"tags":[],"events":["cert"]}`, &ch); code != http.StatusOK || len(ch.Tags) != 0 || len(ch.Events) != 1 || ch.Events[0] != "cert" {
		t.Errorf("update = %d %+v", code, ch)
	}

	// Where a monitor's alerts go: the enabled channels whose scope covers it.
	ctx := context.Background()
	tagged, err := db.CreateMonitor(ctx, store.Monitor{Name: "api", Type: store.MonitorTCP, Target: "a:1", Namespace: "shop", Tags: []string{"pager"},
		Interval: time.Minute, Timeout: time.Second, FailureThreshold: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []store.Channel{
		{Name: "shop-only", Type: store.ChannelWebhook, Target: "http://x", Namespaces: []string{"shop"}, Enabled: true},
		{Name: "pager-only", Type: store.ChannelWebhook, Target: "http://x", Tags: []string{"pager"}, Enabled: true},
		{Name: "billing", Type: store.ChannelWebhook, Target: "http://x", Namespaces: []string{"billing"}, Enabled: true},
		{Name: "off", Type: store.ChannelWebhook, Target: "http://x", Enabled: false},
	} {
		if _, err := db.CreateChannel(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	var where []monitorChannelResponse
	if code := do(t, http.MethodGet, srv.URL+"/api/v1/monitors/"+itoa(tagged.ID)+"/channels", "", &where); code != http.StatusOK {
		t.Fatalf("monitor channels = %d", code)
	}
	names := make([]string, 0, len(where))
	for _, w := range where {
		names = append(names, w.Name)
	}
	if len(names) != 3 || names[0] != "pager" || names[1] != "shop-only" || names[2] != "pager-only" {
		t.Errorf("monitor channels = %v, want [pager shop-only pager-only]", names)
	}
}
