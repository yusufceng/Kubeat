package api

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMaintenanceAPI(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/api/v1"
	do(t, "POST", base+"/monitors", validMonitor, nil)

	var list []maintenanceResponse
	if code := do(t, "GET", base+"/maintenance", "", &list); code != http.StatusOK || list == nil || len(list) != 0 {
		t.Fatalf("empty list: status=%d body=%v", code, list)
	}

	// A window that starts now, given as a duration, for one monitor.
	var silence maintenanceResponse
	before := time.Now()
	if code := do(t, "POST", base+"/maintenance", `{"name":" Susturma ","monitor_id":1,"duration_seconds":3600}`, &silence); code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", code)
	}
	if silence.Name != "Susturma" || silence.State != maintenanceActive || silence.MonitorID == nil || *silence.MonitorID != 1 || silence.MonitorName != "api" {
		t.Errorf("created = %+v", silence)
	}
	if silence.StartsAt.Before(before.Add(-time.Second)) || silence.EndsAt.Sub(silence.StartsAt) != time.Hour {
		t.Errorf("window = %s .. %s, want one hour from now", silence.StartsAt, silence.EndsAt)
	}

	// The monitor reports it.
	var m monitorResponse
	if do(t, "GET", base+"/monitors/1", "", &m); m.MaintenanceUntil == nil || !m.MaintenanceUntil.Equal(silence.EndsAt) {
		t.Errorf("monitor maintenance_until = %v, want %s", m.MaintenanceUntil, silence.EndsAt)
	}

	// An upcoming window with explicit times for a namespace, and a past one
	// cannot be created.
	start := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	body := `{"name":"deploy","namespace":"shop","starts_at":"` + start.Format(time.RFC3339) + `","ends_at":"` + start.Add(2*time.Hour).Format(time.RFC3339) + `"}`
	var upcoming maintenanceResponse
	if code := do(t, "POST", base+"/maintenance", body, &upcoming); code != http.StatusCreated || upcoming.State != maintenanceUpcoming || upcoming.Namespace != "shop" || upcoming.MonitorID != nil {
		t.Errorf("upcoming: status=%d %+v", code, upcoming)
	}

	if do(t, "GET", base+"/maintenance", "", &list); len(list) != 2 || list[0].ID != upcoming.ID || list[1].State != maintenanceActive {
		t.Errorf("list = %+v, want the later start first", list)
	}

	past := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	invalid := map[string]string{
		"no name":            `{"duration_seconds":60}`,
		"no end":             `{"name":"x"}`,
		"both end forms":     `{"name":"x","duration_seconds":60,"ends_at":"` + start.Format(time.RFC3339) + `"}`,
		"negative duration":  `{"name":"x","duration_seconds":-60}`,
		"two scopes":         `{"name":"x","duration_seconds":60,"namespace":"shop","monitor_id":1}`,
		"unknown monitor":    `{"name":"x","duration_seconds":60,"monitor_id":999}`,
		"ends before starts": `{"name":"x","starts_at":"` + start.Format(time.RFC3339) + `","ends_at":"` + start.Add(-time.Hour).Format(time.RFC3339) + `"}`,
		"already over":       `{"name":"x","starts_at":"` + past + `","duration_seconds":60}`,
		"unknown field":      `{"name":"x","duration_seconds":60,"recurring":true}`,
		"not JSON":           `{`,
	}
	for name, body := range invalid {
		if code := do(t, "POST", base+"/maintenance", body, nil); code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, code)
		}
	}

	// A window that started in the past but is still running is fine.
	running := `{"name":"running","starts_at":"` + past + `","duration_seconds":` + "10800" + `}`
	if code := do(t, "POST", base+"/maintenance", running, nil); code != http.StatusCreated {
		t.Errorf("window that started earlier and is still running: status = %d, want 201", code)
	}

	// Ending the silence early.
	if code := do(t, "DELETE", base+"/maintenance/"+strings.TrimSpace("1"), "", nil); code != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", code)
	}
	if code := do(t, "DELETE", base+"/maintenance/1", "", nil); code != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", code)
	}
	// The global "running" window still covers the monitor.
	if do(t, "GET", base+"/monitors/1", "", &m); m.MaintenanceUntil == nil {
		t.Error("monitor left maintenance although a global window is running")
	}
	do(t, "DELETE", base+"/maintenance/3", "", nil)
	if do(t, "GET", base+"/monitors/1", "", &m); m.MaintenanceUntil != nil {
		t.Errorf("maintenance_until after every covering window ended = %v", m.MaintenanceUntil)
	}
}
