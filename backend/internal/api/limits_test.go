package api

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSlots(t *testing.T) {
	s := newSlots(1, 20*time.Millisecond)
	release, ok := s.acquire(context.Background())
	if !ok {
		t.Fatal("the free slot was not given")
	}
	start := time.Now()
	if _, ok := s.acquire(context.Background()); ok {
		t.Fatal("a second slot was given out of one")
	}
	if waited := time.Since(start); waited < 20*time.Millisecond {
		t.Errorf("gave up after %s, before the wait was over", waited)
	}
	// Giving a slot back twice frees it once.
	release()
	release()
	if _, ok := s.acquire(context.Background()); !ok {
		t.Fatal("the slot given back is not free")
	}
	if _, ok := s.acquire(context.Background()); ok {
		t.Error("giving back twice freed two slots")
	}

	// Without a wait, a check waits as long as its request does.
	forever := newSlots(1, 0)
	forever.acquire(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, ok := forever.acquire(ctx); ok {
		t.Error("a slot was given while all were taken")
	}
}

// Without a setting, half the CPUs compare passwords at once, at least
// one, and at least two with a directory: every directory sign-in takes a
// comparison too. A setting is taken as it is.
func TestPasswordCheckSlots(t *testing.T) {
	for _, tc := range []struct {
		setting, procs int
		directory      bool
		want           int
	}{
		{0, 1, false, 1},
		{0, 3, false, 1},
		{0, 8, false, 4},
		{0, 1, true, 2},
		{0, 3, true, 2},
		{0, 8, true, 4},
		{1, 8, true, 1},
		{6, 2, false, 6},
	} {
		if got := passwordCheckSlots(tc.setting, tc.procs, tc.directory); got != tc.want {
			t.Errorf("passwordCheckSlots(%d, %d CPUs, directory %v) = %d, want %d", tc.setting, tc.procs, tc.directory, got, tc.want)
		}
	}
}

func TestAuditCap(t *testing.T) {
	var logs bytes.Buffer
	c := newAuditCap(2, slog.New(slog.NewTextHandler(&logs, nil)))
	var later []func()
	c.later = func(_ time.Duration, f func()) { later = append(later, f) }

	t0 := time.Date(2026, 10, 16, 12, 0, 10, 0, time.UTC)
	var allowed []bool
	for i := range 5 {
		allowed = append(allowed, c.allow(t0.Add(time.Duration(i)*time.Second)))
	}
	if want := []bool{true, true, false, false, false}; !slices.Equal(allowed, want) {
		t.Errorf("in one minute: %v, want %v", allowed, want)
	}
	if len(later) != 1 {
		t.Fatalf("%d reports were scheduled for the minute, want 1", len(later))
	}
	if logs.Len() != 0 {
		t.Errorf("logged before the minute was over: %s", logs.String())
	}
	// The minute is over and nothing came after it: the report says so.
	later[0]()
	if !strings.Contains(logs.String(), "dropped=3") {
		t.Errorf("report = %q, want dropped=3", logs.String())
	}

	// A new minute starts afresh; what it drops is reported by the first
	// failure after it, if that comes before the scheduled report.
	logs.Reset()
	t1 := t0.Add(time.Minute)
	if !c.allow(t1) || !c.allow(t1) || c.allow(t1) {
		t.Error("the next minute did not start afresh")
	}
	if !c.allow(t1.Add(time.Minute)) {
		t.Error("the minute after was refused")
	}
	if !strings.Contains(logs.String(), "dropped=1") {
		t.Errorf("report at the next minute = %q, want dropped=1", logs.String())
	}
	logs.Reset()
	later[1]() // late: already reported
	if logs.Len() != 0 {
		t.Errorf("reported twice: %s", logs.String())
	}

	if unlimited := newAuditCap(0, slog.Default()); !unlimited.allow(t0) || !unlimited.allow(t0) || !unlimited.allow(t0) {
		t.Error("no cap refused")
	}
}
