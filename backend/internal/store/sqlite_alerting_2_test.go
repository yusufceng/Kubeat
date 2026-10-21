package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestChannelTimingAndMonitorSlowState(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	// A channel's delay and repeat interval are kept, in whole seconds.
	ch, err := s.CreateChannel(ctx, Channel{Name: "pager", Type: ChannelPagerDuty, Secret: "key", Enabled: true, Delay: 10 * time.Minute, Repeat: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetChannel(ctx, ch.ID); got.Delay != 10*time.Minute || got.Repeat != time.Hour {
		t.Errorf("channel = %+v, want delay 10m and repeat 1h", got)
	}
	ch.Delay, ch.Repeat = 0, 5*time.Minute
	if updated, err := s.UpdateChannel(ctx, ch); err != nil || updated.Delay != 0 || updated.Repeat != 5*time.Minute {
		t.Errorf("updated channel = %+v, %v", updated, err)
	}

	// A monitor's threshold is a setting; its slow state is the engine's
	// and ends with the threshold or when the monitor is paused.
	m := newMonitor("api")
	m.SlowAbove, m.SlowSince = 800*time.Millisecond, time.Now()
	m, err = s.CreateMonitor(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetMonitor(ctx, m.ID); got.SlowAbove != 800*time.Millisecond || !got.SlowSince.IsZero() || !reflect.DeepEqual(got, m) {
		t.Errorf("created = %+v, want threshold 800ms and no slow state", got)
	}
	since := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	if err := s.SetMonitorSlowSince(ctx, m.ID, since); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetMonitor(ctx, m.ID); !got.SlowSince.Equal(since) {
		t.Errorf("SlowSince = %v, want %v", got.SlowSince, since)
	}
	if statuses, _ := s.ListMonitorStatuses(ctx, since); len(statuses) != 1 || !statuses[0].SlowSince.Equal(since) {
		t.Errorf("statuses = %+v, want the slow state", statuses)
	}
	// Changing something else keeps the state.
	m.Description = "the API"
	if updated, err := s.UpdateMonitor(ctx, m); err != nil || !updated.SlowSince.Equal(since) {
		t.Errorf("after an update = %+v, %v; want the slow state kept", updated, err)
	}
	m.Enabled = false
	if updated, err := s.UpdateMonitor(ctx, m); err != nil || !updated.SlowSince.IsZero() {
		t.Errorf("after pausing = %+v, %v; want the slow state cleared", updated, err)
	}
	m.Enabled = true
	if _, err := s.UpdateMonitor(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMonitorSlowSince(ctx, m.ID, since); err != nil {
		t.Fatal(err)
	}
	m.SlowAbove = 0
	if updated, err := s.UpdateMonitor(ctx, m); err != nil || !updated.SlowSince.IsZero() || updated.SlowAbove != 0 {
		t.Errorf("after turning the threshold off = %+v, %v; want the slow state cleared", updated, err)
	}
	if err := s.SetMonitorSlowSince(ctx, m.ID, time.Time{}); err != nil {
		t.Errorf("clearing: %v", err)
	}
	if err := s.SetMonitorSlowSince(ctx, 999, since); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown monitor: err = %v, want ErrNotFound", err)
	}
}
