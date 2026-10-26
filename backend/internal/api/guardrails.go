package api

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/auth"
	"github.com/yusufceng/Kubeat/backend/internal/guardrails"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// The guardrails that keep monitors from putting load on other systems,
// whether by mistake or on purpose: thousands of monitors against one site
// at the shortest interval would be a denial of service with Kubeat's
// name on it. The rules on a monitor as it is made (which targets are
// external, their shortest interval, the cap per host, the quotas) are the
// guardrails package's, shared with the annotation syncer; the ceiling on
// checks a minute is the scheduler's; what one account may create in a
// minute is bounded here. Each limit is a setting, zero turning it off;
// the README's "Hedef yükü" explains them.

// Throttles says which monitors' checks are waiting for their host's
// ceiling on checks a minute: the scheduler. Nil when there is none.
type Throttles interface {
	// ThrottledSince is since when the monitor's due check has been
	// waiting; the zero time while it is not.
	ThrottledSince(monitorID int64) time.Time
}

// limits are the rules on a monitor as it is made, from the options.
func (h *handler) limits() guardrails.Limits {
	return guardrails.Limits{
		MinInterval: h.opts.MinCheckInterval, MinIntervalExternal: h.opts.MinCheckIntervalExternal,
		MaxMonitors: h.opts.MaxMonitors, MaxMonitorsPerNamespace: h.opts.MaxMonitorsPerNamespace, MaxMonitorsPerHost: h.opts.MaxMonitorsPerHost,
	}
}

// Why a monitor may not be made, as the API says it. The quotas answer
// 422: the request is fine, the installation is full. The cap per host
// answers 400: this target may not have another monitor.
func refusalStatus(r *guardrails.Refusal) int {
	if r.Kind == guardrails.Quota {
		return http.StatusUnprocessableEntity
	}
	return http.StatusBadRequest
}

// admit says whether m may be made, or changed into, given existing, every
// monitor there is (guardrails.Limits.Admit). It returns 0 and "" when it
// may, else the status and the sentence to answer with.
func (h *handler) admit(m store.Monitor, existing []store.Monitor) (int, string) {
	if r := h.limits().Admit(m, existing); r != nil {
		return refusalStatus(r), r.Message
	}
	return 0, ""
}

// createLimiter bounds the monitors one account creates in a minute, the
// API token counting as one account. A burst of creations is the shape of
// the threat the guardrails are for, and the one thing a stolen account
// does that a careful editor never does. The minute slides: it is the last
// sixty seconds at any moment, not the clock's minute, which would let
// twice the limit through around its boundary. A creation is reserved
// before it is attempted and released when it does not happen (a name
// already taken, say), so that a refused request never counts, and
// requests in flight at once cannot slip past the limit together. The
// first refusal of a burst is written to the audit log, so that an
// administrator sees it.
type createLimiter struct {
	perMinute int

	mu       sync.Mutex
	accounts map[string]*creations
}

// creations is one account's last minute.
type creations struct {
	at      []time.Time // creations in the last minute, oldest first
	audited bool        // whether this burst's refusal was written to the audit log
}

func newCreateLimiter(perMinute int) *createLimiter {
	return &createLimiter{perMinute: perMinute, accounts: make(map[string]*creations)}
}

// allow reserves a creation by the account at now and reports whether it
// may go ahead. When it may not, retryAfter is how long until the oldest
// creation of the minute is a minute old, and audit is true for the first
// refusal since the account was last allowed one. Accounts without a
// creation in the last minute are forgotten, so the map holds only the
// accounts that created something lately.
func (l *createLimiter) allow(account string, now time.Time) (ok bool, retryAfter time.Duration, audit bool) {
	if l.perMinute <= 0 {
		return true, 0, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, a := range l.accounts {
		a.at = slices.DeleteFunc(a.at, func(t time.Time) bool { return now.Sub(t) >= time.Minute })
		if len(a.at) == 0 {
			delete(l.accounts, key)
		}
	}
	a, ok := l.accounts[account]
	if !ok {
		a = &creations{}
		l.accounts[account] = a
	}
	if len(a.at) < l.perMinute {
		a.at = append(a.at, now)
		a.audited = false
		return true, 0, false
	}
	audit = !a.audited
	a.audited = true
	return false, a.at[0].Add(time.Minute).Sub(now), audit
}

// release gives back a creation reserved at `at` that did not happen.
func (l *createLimiter) release(account string, at time.Time) {
	if l.perMinute <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.accounts[account]
	if !ok {
		return
	}
	if i := slices.Index(a.at, at); i >= 0 {
		a.at = slices.Delete(a.at, i, i+1)
	}
	if len(a.at) == 0 {
		delete(l.accounts, account)
	}
}

// accountKey is what the creation limit counts by: the user, or the API
// token as one account.
func accountKey(p auth.Principal) string {
	if p.Service {
		return "token"
	}
	return "user:" + strconv.FormatInt(p.UserID, 10)
}

// createThrottled is the audit phrase of a burst in which an account hit
// the creation limit; its count is the limit.
const createThrottled = "%d monitors created within a minute, the limit (UPTIME_MONITOR_CREATE_PER_MINUTE); further creations refused until the oldest is a minute old"

// creationAllowed reserves a creation by the request's account and reports
// whether it may go ahead; when it may, release gives the reservation back
// should the creation not happen. When it may not, retryAfter is after how
// many seconds it may again, at least one. The first refusal of a burst
// goes to the audit log.
func (h *handler) creationAllowed(r *http.Request) (ok bool, retryAfter int, release func()) {
	account, now := accountKey(principal(r)), time.Now()
	ok, wait, audit := h.creations.allow(account, now)
	if ok {
		return true, 0, func() { h.creations.release(account, now) }
	}
	if audit {
		h.audit(r, "monitor_create_throttled", "monitor", 0, fmt.Sprintf(h.auditText(createThrottled), h.opts.MonitorCreatePerMinute))
	}
	return false, max(1, int(wait.Round(time.Second)/time.Second)), nil
}

// creationRefused is the sentence of a creation the limit a minute refused.
func (h *handler) creationRefused(retryAfter int) string {
	return fmt.Sprintf("the limit of %d monitors created within a minute is reached (UPTIME_MONITOR_CREATE_PER_MINUTE); try again in %d s",
		h.opts.MonitorCreatePerMinute, retryAfter)
}

// allowCreation is creationAllowed for a request that creates one monitor:
// when it may not, it answers 429 with Retry-After, and the caller just
// returns. When it may, release gives the reservation back should the
// creation not happen.
func (h *handler) allowCreation(w http.ResponseWriter, r *http.Request) (release func(), ok bool) {
	ok, retryAfter, release := h.creationAllowed(r)
	if ok {
		return release, true
	}
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	writeError(w, http.StatusTooManyRequests, h.creationRefused(retryAfter))
	return nil, false
}

// monitorResponse is the monitor as the principal sees it, with whether its
// check is waiting for its host's ceiling, which the scheduler knows.
func (h *handler) monitorResponse(p auth.Principal, st store.MonitorStatus, window time.Duration) monitorResponse {
	resp := toMonitorResponse(p, st, window)
	if h.opts.Throttles != nil && st.Enabled {
		if since := h.opts.Throttles.ThrottledSince(st.ID); !since.IsZero() {
			resp.ThrottledSince = &since
		}
	}
	return resp
}
