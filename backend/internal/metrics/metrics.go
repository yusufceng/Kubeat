// Package metrics exposes what the server is doing in the Prometheus
// exposition format, so the tool that watches everything can itself be
// watched.
package metrics

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// Metrics records events. A nil *Metrics is valid and records nothing, so
// callers do not need to check whether metrics are enabled.
type Metrics struct {
	registry *prometheus.Registry

	checks            *prometheus.CounterVec
	checkDuration     *prometheus.HistogramVec
	checksInFlight    prometheus.Gauge
	dispatchSkipped   prometheus.Counter
	lastTick          prometheus.Gauge
	resultsWritten    prometheus.Counter
	resultsDropped    prometheus.Counter
	notifications     *prometheus.CounterVec
	notificationAtt   *prometheus.CounterVec
	notificationsDrop prometheus.Counter
	heartbeats        *prometheus.CounterVec
	lastHeartbeat     prometheus.Gauge
	backups           *prometheus.CounterVec
	lastBackup        prometheus.Gauge
	lastBackupFailure prometheus.Gauge
	licenseExpiry     prometheus.Gauge
	targetThrottled   *prometheus.CounterVec

	// hosts are the host labels targetThrottled has been given so far; past
	// MaxThrottledHosts of them, a new host is counted as OtherHost.
	mu    sync.Mutex
	hosts map[string]bool

	// undelivered are the outages each channel, by ID, has not taken and
	// is still to be sent again, as the alert engine last said
	// (UndeliveredOutages); read at scrape time.
	undeliveredMu sync.Mutex
	undelivered   map[int64]int
}

// MaxThrottledHosts bounds the host labels of kubeat_target_throttled_total:
// the first this many hosts whose checks had to wait get a label of
// their own, every host after them is counted under OtherHost. A label per
// host would otherwise let a flood of monitors against made-up hosts grow
// the metric without end.
const MaxThrottledHosts = 100

// OtherHost is the host label of the hosts past MaxThrottledHosts.
const OtherHost = "other"

// New returns Metrics with the Go runtime and process collectors registered.
func New(version string) *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		checks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubeat_checks_total",
			Help: "Checks run, by monitor type and outcome.",
		}, []string{"type", "result"}),
		checkDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "kubeat_check_duration_seconds",
			Help:    "How long checks took, by monitor type.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30},
		}, []string{"type"}),
		checksInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kubeat_checks_in_flight",
			Help: "Checks running right now.",
		}),
		dispatchSkipped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kubeat_scheduler_dispatch_skipped_total",
			Help: "Times a due monitor was left for the next tick because the workers stayed busy for a whole tick. A steady increase means UPTIME_WORKERS is too low.",
		}),
		lastTick: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kubeat_scheduler_last_tick_timestamp_seconds",
			Help: "When the scheduler last completed a tick. A value that stops advancing means checks are not being scheduled.",
		}),
		resultsWritten: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kubeat_results_written_total",
			Help: "Check results stored.",
		}),
		resultsDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kubeat_results_dropped_total",
			Help: "Check results that could not be stored.",
		}),
		notifications: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubeat_notifications_total",
			Help: "Notifications by channel type, event and final outcome: sent once a channel took it, failed once the engine stopped trying (after the tries of one attempt, and for an outage notification after the attempts spread over the outage).",
		}, []string{"channel_type", "event", "result"}),
		notificationAtt: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubeat_notification_attempts_total",
			Help: "Attempts to deliver a notification to a channel, by channel type and outcome: one per entry of the delivery log, the attempts of an outage notification sent again included.",
		}, []string{"channel_type", "result"}),
		notificationsDrop: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kubeat_notifications_dropped_total",
			Help: "Notifications dropped because the delivery queue was full.",
		}),
		heartbeats: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubeat_heartbeats_total",
			Help: "Heartbeats to the external dead man's switch, by outcome: sent, failed, or withheld because the scheduler was not healthy.",
		}, []string{"result"}),
		lastHeartbeat: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kubeat_heartbeat_last_success_timestamp_seconds",
			Help: "When a heartbeat was last accepted by the dead man's switch.",
		}),
	}

	m.backups = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kubeat_backups_total",
		Help: "Database backups attempted, by outcome.",
	}, []string{"result"})
	m.targetThrottled = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kubeat_target_throttled_total",
		Help: "Waits begun by checks whose target host, a host their last check landed on, or an address it reached, had reached its checks-per-minute ceiling (UPTIME_TARGET_MAX_RATE, UPTIME_CLUSTER_TARGET_MAX_RATE, UPTIME_TARGET_ADDRESS_MAX_RATE): one per wait, however many ticks it lasts, by the monitor's host; hosts past the first 100 are counted as \"other\".",
	}, []string{"host"})
	m.hosts = make(map[string]bool)
	m.lastBackup = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "kubeat_backup_last_success_timestamp_seconds",
		Help: "When the newest database backup was taken: the last to succeed, or after a start the newest copy found in the backup directory; 0 while there is none.",
	})
	m.lastBackupFailure = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "kubeat_backup_last_failure_timestamp_seconds",
		Help: "When a database backup attempt last failed since the process started; 0 while none has. Newer than kubeat_backup_last_success_timestamp_seconds while the backups are failing.",
	})

	// A labelled counter has no series until its first increase, and
	// increase() cannot tell a first increase from a series that appears:
	// the series the alert rules watch are there from the start, at 0.
	for _, channelType := range store.ChannelTypes {
		for _, delivered := range []bool{true, false} {
			m.notificationAtt.WithLabelValues(string(channelType), resultLabel(delivered, "sent", "failed"))
		}
	}
	for _, ok := range []bool{true, false} {
		m.backups.WithLabelValues(resultLabel(ok, "ok", "failed"))
	}

	m.licenseExpiry = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "kubeat_license_expiry_timestamp_seconds",
		Help: "When the Kubeat Enterprise license ends; 0 without a usable license.",
	})

	build := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "kubeat_build_info",
		Help:        "Always 1; the version is in the label.",
		ConstLabels: prometheus.Labels{"version": version},
	})
	build.Set(1)

	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		build,
		m.checks, m.checkDuration, m.checksInFlight, m.dispatchSkipped, m.lastTick,
		m.resultsWritten, m.resultsDropped,
		m.notifications, m.notificationAtt, m.notificationsDrop,
		m.heartbeats, m.lastHeartbeat,
		m.backups, m.lastBackup, m.lastBackupFailure,
		m.targetThrottled,
		m.licenseExpiry,
	)
	return m
}

// TargetThrottled records a check of a monitor of host that began to wait
// because a host or an address it goes to had reached its ceiling of
// checks a minute.
func (m *Metrics) TargetThrottled(host string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if !m.hosts[host] {
		if len(m.hosts) >= MaxThrottledHosts {
			host = OtherHost
		} else {
			m.hosts[host] = true
		}
	}
	m.mu.Unlock()
	m.targetThrottled.WithLabelValues(host).Inc()
}

// WatchHealth adds kubeat_self_healthy, read from healthy on every scrape: 1
// while it returns nil, 0 while it says why Kubeat is not doing its job.
// main gives it the judgement the heartbeat sends on, so that a cluster's
// own Prometheus hears what an external dead man's switch would, with or
// without one.
func (m *Metrics) WatchHealth(healthy func() error) {
	if m == nil {
		return
	}
	m.registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "kubeat_self_healthy",
		Help: "1 while Kubeat is doing its job, 0 while not, judged at every scrape as the heartbeat judges it: 0 when the scheduler completed no tick over the last UPTIME_HEARTBEAT_INTERVAL, or check results came in that time and none of them could be stored, or one write of them has outlasted it. An interval in which no result came is healthy.",
	}, func() float64 {
		if healthy() != nil {
			return 0
		}
		return 1
	}))
}

// Handler serves the metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Gather returns the current metrics; used by tests.
func (m *Metrics) Gather() (map[string]float64, error) {
	families, err := m.registry.Gather()
	if err != nil {
		return nil, err
	}
	out := make(map[string]float64)
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			key := family.GetName()
			for _, label := range metric.GetLabel() {
				key += "{" + label.GetName() + "=" + label.GetValue() + "}"
			}
			switch {
			case metric.Counter != nil:
				out[key] = metric.Counter.GetValue()
			case metric.Gauge != nil:
				out[key] = metric.Gauge.GetValue()
			case metric.Histogram != nil:
				out[key+"_count"] = float64(metric.Histogram.GetSampleCount())
			}
		}
	}
	return out, nil
}

func resultLabel(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

// CheckStarted and CheckDone bracket one check.
func (m *Metrics) CheckStarted() {
	if m != nil {
		m.checksInFlight.Inc()
	}
}

func (m *Metrics) CheckDone(monitorType string, up bool, took time.Duration) {
	if m == nil {
		return
	}
	m.checksInFlight.Dec()
	m.checks.WithLabelValues(monitorType, resultLabel(up, "up", "down")).Inc()
	m.checkDuration.WithLabelValues(monitorType).Observe(took.Seconds())
}

// SchedulerTick records a completed scheduler tick.
func (m *Metrics) SchedulerTick(at time.Time) {
	if m != nil {
		m.lastTick.Set(float64(at.UnixMilli()) / 1000)
	}
}

// DispatchSkipped records due monitors left waiting for a free worker.
func (m *Metrics) DispatchSkipped(n int) {
	if m != nil && n > 0 {
		m.dispatchSkipped.Add(float64(n))
	}
}

// Results records how many check results were stored and how many lost.
func (m *Metrics) Results(written, dropped int) {
	if m == nil {
		return
	}
	m.resultsWritten.Add(float64(written))
	m.resultsDropped.Add(float64(dropped))
}

// Notification records the final outcome of delivering to one channel:
// taken, or given up on after every attempt.
func (m *Metrics) Notification(channelType, event string, delivered bool) {
	if m != nil {
		m.notifications.WithLabelValues(channelType, event, resultLabel(delivered, "sent", "failed")).Inc()
	}
}

// NotificationAttempt records one attempt to deliver to one channel, as
// the delivery log does; an outage notification sent again is counted
// once more.
func (m *Metrics) NotificationAttempt(channelType string, delivered bool) {
	if m != nil {
		m.notificationAtt.WithLabelValues(channelType, resultLabel(delivered, "sent", "failed")).Inc()
	}
}

// NotificationDropped records a notification lost to a full queue.
func (m *Metrics) NotificationDropped() {
	if m != nil {
		m.notificationsDrop.Inc()
	}
}

// UndeliveredOutages records, by channel ID, how many outages each channel
// has not taken and is still to be sent again: the alert engine's list of
// what it sends again, as it stands. A channel missing from it has none.
// The scrape reports it per channel (kubeat_channel_undelivered_outages).
func (m *Metrics) UndeliveredOutages(byChannel map[int64]int) {
	if m == nil {
		return
	}
	counts := make(map[int64]int, len(byChannel))
	for id, n := range byChannel {
		counts[id] = n
	}
	m.undeliveredMu.Lock()
	m.undelivered = counts
	m.undeliveredMu.Unlock()
}

// undeliveredOutages is the channel's count from UndeliveredOutages.
func (m *Metrics) undeliveredOutages() map[int64]int {
	m.undeliveredMu.Lock()
	defer m.undeliveredMu.Unlock()
	return m.undelivered
}

// Heartbeat outcomes.
const (
	HeartbeatSent     = "sent"
	HeartbeatFailed   = "failed"
	HeartbeatWithheld = "withheld"
)

// Heartbeat records one heartbeat attempt.
func (m *Metrics) Heartbeat(result string, at time.Time) {
	if m == nil {
		return
	}
	m.heartbeats.WithLabelValues(result).Inc()
	if result == HeartbeatSent {
		m.lastHeartbeat.Set(float64(at.UnixMilli()) / 1000)
	}
}

// License records when the license ends; the zero time without one.
func (m *Metrics) License(expires time.Time) {
	if m == nil {
		return
	}
	if expires.IsZero() {
		m.licenseExpiry.Set(0)
		return
	}
	m.licenseExpiry.Set(float64(expires.Unix()))
}

// Backup records one backup attempt. A failure is kept with its time, so
// that the backups read as failing until a newer one succeeds, however long
// the next attempt waits.
func (m *Metrics) Backup(ok bool, at time.Time) {
	if m == nil {
		return
	}
	m.backups.WithLabelValues(resultLabel(ok, "ok", "failed")).Inc()
	if ok {
		m.lastBackup.Set(float64(at.UnixMilli()) / 1000)
	} else {
		m.lastBackupFailure.Set(float64(at.UnixMilli()) / 1000)
	}
}

// LastBackup records when the newest backup already in the backup directory
// was taken, so that the gauge does not read 0 from a restart until the next
// backup. Unlike Backup it counts no attempt: none was made.
func (m *Metrics) LastBackup(at time.Time) {
	if m != nil {
		m.lastBackup.Set(float64(at.UnixMilli()) / 1000)
	}
}

// StatusLister is the part of the store read at scrape time: the monitors,
// and the channels with their last delivery.
type StatusLister interface {
	ListMonitorStatuses(ctx context.Context, since time.Time) ([]store.MonitorStatus, error)
	ListChannels(ctx context.Context) ([]store.Channel, error)
}

var (
	// The monitor's namespace is not called "namespace": a scraping
	// Prometheus sets that label to where the target runs (OpenShift's user
	// workload monitoring enforces it), and the monitor's would be pushed
	// aside to "exported_namespace".
	monitorLabels = []string{"monitor", "monitor_namespace", "type"}

	monitorUp = prometheus.NewDesc("kubeat_monitor_up",
		"1 if the monitor's latest check succeeded, 0 if it failed. Absent for monitors that are paused or not checked yet.", monitorLabels, nil)
	monitorLatency = prometheus.NewDesc("kubeat_monitor_response_seconds",
		"How long the monitor's latest check took.", monitorLabels, nil)
	monitorLastCheck = prometheus.NewDesc("kubeat_monitor_last_check_timestamp_seconds",
		"When the monitor was last checked.", monitorLabels, nil)
	monitorIncident = prometheus.NewDesc("kubeat_monitor_incident_open",
		"1 while the monitor has an open outage.", monitorLabels, nil)
	monitorSlow = prometheus.NewDesc("kubeat_monitor_slow",
		"1 while the monitor answers, but slower than its slow_above_ms threshold; 0 otherwise, also for monitors without a threshold.", monitorLabels, nil)
	monitorCertExpiry = prometheus.NewDesc("kubeat_monitor_cert_expiry_timestamp_seconds",
		"When the certificate seen by the monitor's latest check expires.", monitorLabels, nil)
	monitorsTotal = prometheus.NewDesc("kubeat_monitors",
		"Number of monitors, by whether they are enabled.", []string{"state"}, nil)
	scrapeOK = prometheus.NewDesc("kubeat_monitor_scrape_success",
		"1 if the per-monitor and per-channel metrics could be read from the database for this scrape.", nil, nil)

	// A channel by its name, which is unique, and its type.
	channelLabels = []string{"channel", "channel_type"}

	channelDelivered = prometheus.NewDesc("kubeat_channel_last_delivery_ok",
		"1 if the last attempt to deliver a notification to the channel succeeded, 0 if it failed, as the channel's row says. Only for enabled channels attempted at least once.", channelLabels, nil)
	channelUndelivered = prometheus.NewDesc("kubeat_channel_undelivered_outages",
		"Outages (down notifications) the channel has not taken and is still to be sent again: ones it did not take, and ones held back for it while it is paused; 0 when there are none. For every enabled channel attempted at least once or owed such an outage.", channelLabels, nil)
)

// collectChannels reports, for every enabled channel attempted at least
// once, its last delivery from its row and the outages it has not taken
// from the alert engine. A channel disabled, or deleted, leaves at once.
func (c *monitorCollector) collectChannels(channels []store.Channel, gauge func(*prometheus.Desc, float64, ...string)) {
	undelivered := c.metrics.undeliveredOutages()
	for _, channel := range channels {
		pending := undelivered[channel.ID]
		if !channel.Enabled || channel.LastAttemptAt.IsZero() && pending == 0 {
			continue
		}
		labels := []string{channel.Name, string(channel.Type)}
		if !channel.LastAttemptAt.IsZero() {
			ok := 0.0
			if channel.LastOK {
				ok = 1
			}
			gauge(channelDelivered, ok, labels...)
		}
		gauge(channelUndelivered, float64(pending), labels...)
	}
}

type monitorCollector struct {
	store   StatusLister
	timeout time.Duration
	logger  *slog.Logger
	metrics *Metrics
}

// WatchMonitors adds per-monitor and per-channel metrics, read from the
// store on every scrape with the given timeout.
func (m *Metrics) WatchMonitors(st StatusLister, timeout time.Duration, logger *slog.Logger) {
	m.registry.MustRegister(&monitorCollector{store: st, timeout: timeout, logger: logger, metrics: m})
}

func (c *monitorCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{monitorUp, monitorLatency, monitorLastCheck, monitorIncident, monitorSlow, monitorCertExpiry, monitorsTotal, scrapeOK,
		channelDelivered, channelUndelivered} {
		ch <- d
	}
}

func (c *monitorCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	// "since now" leaves the statistics window empty: only the latest result
	// and the incident state are needed here.
	statuses, err := c.store.ListMonitorStatuses(ctx, time.Now())
	if err != nil {
		c.logger.Error("reading monitors for metrics failed", "error", err)
	}
	channels, channelsErr := c.store.ListChannels(ctx)
	if channelsErr != nil {
		c.logger.Error("reading channels for metrics failed", "error", channelsErr)
	}
	// The alert rules hold the last values of what a scrape lacks only
	// while this is 0 (KubeatMonitorDown, KubeatNotificationsFailing).
	ok := 0.0
	if err == nil && channelsErr == nil {
		ok = 1
	}
	ch <- prometheus.MustNewConstMetric(scrapeOK, prometheus.GaugeValue, ok)

	gauge := func(desc *prometheus.Desc, value float64, labels ...string) {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value, labels...)
	}
	if channelsErr == nil {
		c.collectChannels(channels, gauge)
	}
	if err != nil {
		return
	}
	seconds := func(t time.Time) float64 { return float64(t.UnixMilli()) / 1000 }

	enabled, paused := 0, 0
	for _, st := range statuses {
		if !st.Enabled {
			paused++
			continue
		}
		enabled++
		labels := []string{st.Name, st.Namespace, string(st.Type)}

		incident := 0.0
		if st.Incident != nil {
			incident = 1
		}
		gauge(monitorIncident, incident, labels...)
		slow := 0.0
		if !st.SlowSince.IsZero() {
			slow = 1
		}
		gauge(monitorSlow, slow, labels...)

		if st.Last == nil {
			continue
		}
		up := 0.0
		if st.Last.Up {
			up = 1
		}
		gauge(monitorUp, up, labels...)
		gauge(monitorLatency, st.Last.Latency.Seconds(), labels...)
		gauge(monitorLastCheck, seconds(st.Last.CheckedAt), labels...)
		if !st.Last.CertExpiresAt.IsZero() {
			gauge(monitorCertExpiry, seconds(st.Last.CertExpiresAt), labels...)
		}
	}
	gauge(monitorsTotal, float64(enabled), "enabled")
	gauge(monitorsTotal, float64(paused), "paused")
}
