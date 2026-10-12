package store

import (
	"sort"
	"time"
)

// Repeat is how often a maintenance window comes back.
type Repeat string

const (
	RepeatNone    Repeat = ""        // it happens once
	RepeatDaily   Repeat = "daily"   // every day at the same time
	RepeatWeekly  Repeat = "weekly"  // every week on the same weekday
	RepeatMonthly Repeat = "monthly" // every month on the same day; months without that day are skipped
)

// Period is the time between two occurrences, or for months the shortest
// such time. A window must be shorter than it.
func (r Repeat) Period() time.Duration {
	const day = 24 * time.Hour
	switch r {
	case RepeatDaily:
		return day
	case RepeatWeekly:
		return 7 * day
	case RepeatMonthly:
		return 28 * day
	default:
		return 0
	}
}

// Covers reports whether the window is about the monitor.
func (w Maintenance) Covers(m Monitor) bool {
	switch {
	case w.MonitorID != 0:
		return w.MonitorID == m.ID
	case w.Namespace != "":
		return w.Namespace == m.Namespace
	default:
		return true
	}
}

// Occurrence returns the occurrence of the window that covers at or, when
// none does, the next one after at. ok is false when the window is over
// for good.
func (w Maintenance) Occurrence(at time.Time) (start, end time.Time, ok bool) {
	if w.Repeat == RepeatNone || at.Before(w.StartsAt) {
		return w.StartsAt, w.EndsAt, at.Before(w.EndsAt)
	}

	loc, err := time.LoadLocation(w.Timezone)
	if err != nil {
		loc = time.UTC
	}
	first, local := w.StartsAt.In(loc), at.In(loc)
	length := w.EndsAt.Sub(w.StartsAt)
	// The same time of day as the first occurrence, on another date. Go
	// moves a day that the month lacks into the next month; such a date is
	// not an occurrence.
	on := func(year int, month time.Month, day int) (time.Time, bool) {
		t := time.Date(year, month, day, first.Hour(), first.Minute(), first.Second(), 0, loc)
		return t, t.Day() == day || w.Repeat != RepeatMonthly
	}

	// The occurrences around at: one may have begun before it and still be
	// going on, and the next may be a period or, for months, two away.
	var starts []time.Time
	for k := -1; k <= 3; k++ {
		var (
			t      time.Time
			exists bool
		)
		switch w.Repeat {
		case RepeatDaily:
			t, exists = on(local.Year(), local.Month(), local.Day()+k)
		case RepeatWeekly:
			back := (int(local.Weekday()) - int(first.Weekday()) + 7) % 7
			t, exists = on(local.Year(), local.Month(), local.Day()-back+7*k)
		case RepeatMonthly:
			// From the first of the month, so that adding months cannot
			// itself overflow.
			month := time.Date(local.Year(), local.Month()+time.Month(k), 1, 0, 0, 0, 0, loc)
			t, exists = on(month.Year(), month.Month(), first.Day())
		}
		if exists && !t.Before(w.StartsAt) {
			starts = append(starts, t)
		}
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i].Before(starts[j]) })
	for _, s := range starts {
		if e := s.Add(length); at.Before(e) {
			return s.UTC(), e.UTC(), true
		}
	}
	return time.Time{}, time.Time{}, false
}

// ActiveUntil reports whether the window covers at and, if so, when the
// occurrence that does ends.
func (w Maintenance) ActiveUntil(at time.Time) (time.Time, bool) {
	start, end, ok := w.Occurrence(at)
	if !ok || at.Before(start) {
		return time.Time{}, false
	}
	return end, true
}
