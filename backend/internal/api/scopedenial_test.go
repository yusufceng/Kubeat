package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/yusufceng/Kubeat/backend/internal/metrics"
)

// answeringResolver answers a name from its table, with addresses or with
// an error, as the backend's resolver does: in the cluster for a two-label
// name the search domains (ndots) turn into a Service, a SERVFAIL or a
// timeout of its own for a name its upstream does not answer for; an
// unknown name does not exist (NXDOMAIN).
type answeringResolver map[string]any

func (r answeringResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	switch answer := r[host].(type) {
	case []netip.Addr:
		return answer, nil
	case error:
		return nil, answer
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

var (
	servfail     = &net.DNSError{Err: "server misbehaving", Name: "x", IsTemporary: true}
	resolverSlow = &net.DNSError{Err: "i/o timeout", Name: "x", IsTimeout: true}
)

// The API tells the operator why it refused a scoped user's target: one that
// leads outside their namespaces counts as target, one the resolver could
// not answer for — a SERVFAIL, an NXDOMAIN, a timeout of the resolver or
// the end of UPTIME_TARGET_RESOLVE_TIMEOUT — as resolve_error, each a
// user's, and each leaves a deny row saying which. The forms refused by
// their spelling are never resolved, so they count as target during an
// outage too: a bare address, a short name, another namespace's .svc and
// full name, the API server's names. A two-label name the search domains
// (ndots) turn into a Service, and a public name pointing into the cluster,
// count as target where the resolver answers and as resolve_error where it
// does not. The client is told neither: every refusal is the same 403 with
// the same body.
func TestScopeDenialTellsResolveError(t *testing.T) {
	spelled := []string{
		"https://172.30.0.1/", "https://[fd00::1]/", // bare cluster addresses
		"https://api/", "https://kubernetes/", // short names
		"https://api.payments.svc/", "https://api.payments.svc.cluster.local/", // another namespace's Service
		"https://kubernetes.default.svc/", "https://kubernetes.default.svc.cluster.local/", // the API server
	}
	resolved := []string{"https://api.payments/", "https://kubernetes.default/", "https://172-30-0-1.sslip.io/"}
	unresolved := []string{"https://servfail.example.com/", "https://nowhere.example.com/", "https://slow.example.com/", "https://expired.example.com/"}
	answers := answeringResolver{
		"api.payments":         ipList(t, "172.30.44.9"),
		"kubernetes.default":   ipList(t, "172.30.0.1"),
		"172-30-0-1.sslip.io":  ipList(t, "172.30.0.1"),
		"servfail.example.com": servfail,
		"slow.example.com":     resolverSlow,
		"expired.example.com":  context.DeadlineExceeded, // as the end of UPTIME_TARGET_RESOLVE_TIMEOUT
	}

	for name, tc := range map[string]struct {
		resolver   answeringResolver
		target     []string
		unresolved []string
		language   string
	}{
		"resolver answers": {answers, append(append([]string{}, spelled...), resolved...), unresolved, "en"},
		"outage":           {answeringResolver{"api.payments": servfail, "kubernetes.default": servfail, "172-30-0-1.sslip.io": servfail}, spelled, resolved, "tr"},
	} {
		t.Run(name, func(t *testing.T) {
			meter := metrics.New("test")
			srv, db := newTestAPIWith(t, func(o *Options) {
				o.Resolver, o.Meter, o.Language = tc.resolver, meter, tc.language
			})
			base := srv.URL
			token := signInScoped(t, base)

			var bodies []errorBody
			for i, target := range append(append([]string{}, tc.target...), tc.unresolved...) {
				var problem errorBody
				if code := doAsError(t, token, "POST", base+"/api/v1/monitors", monitorJSON("m-"+itoa(int64(i)), "http", target, `"namespace":"shop"`), &problem); code != http.StatusForbidden {
					t.Errorf("%s: status = %d, want 403", target, code)
				}
				bodies = append(bodies, problem)
			}
			// A channel's webhook alike.
			hook := `{"name":"hook","type":"webhook","target":"` + tc.unresolved[0] + `x","namespaces":["shop"]}`
			var problem errorBody
			if code := doAsError(t, token, "POST", base+"/api/v1/channels", hook, &problem); code != http.StatusForbidden {
				t.Errorf("webhook at %s: status = %d, want 403", tc.unresolved[0], code)
			}
			bodies = append(bodies, problem)
			for _, body := range bodies {
				if body != (errorBody{Error: scopeRefused, Code: codeTargetScope}) {
					t.Errorf("body = %+v, want the generic refusal", body)
				}
			}

			got, err := meter.Gather()
			if err != nil {
				t.Fatal(err)
			}
			if n := got[scopeDeniedKey(metrics.ScopeDeniedTarget)]; n != float64(len(tc.target)) {
				t.Errorf("reason=target = %v, want %d", n, len(tc.target))
			}
			if n := got[scopeDeniedKey(metrics.ScopeDeniedResolveError)]; n != float64(len(tc.unresolved)+1) {
				t.Errorf("reason=resolve_error = %v, want %d", n, len(tc.unresolved)+1)
			}
			for key, n := range got {
				if strings.HasPrefix(key, "kubeat_target_scope_denied_total") && !strings.HasSuffix(key, "{source=user}") && n != 0 {
					t.Errorf("%s = %v: the API's refusals are a user's", key, n)
				}
			}

			outside, unknown := "is outside your namespaces", "did not resolve"
			if tc.language == "tr" {
				outside, unknown = "namespace'lerinizin dışında", "çözülemedi"
			}
			rows := auditDenials(t, db)
			if len(rows) != len(tc.target)+len(tc.unresolved)+1 {
				t.Fatalf("deny rows = %d, want %d", len(rows), len(tc.target)+len(tc.unresolved)+1)
			}
			saidOutside, saidUnknown := 0, 0
			for _, row := range rows {
				switch {
				case strings.Contains(row.Summary, outside):
					saidOutside++
				case strings.Contains(row.Summary, unknown):
					saidUnknown++
				default:
					t.Errorf("deny row %q says neither", row.Summary)
				}
			}
			if saidOutside != len(tc.target) || saidUnknown != len(tc.unresolved)+1 {
				t.Errorf("deny rows say outside %d times and did not resolve %d times, want %d and %d",
					saidOutside, saidUnknown, len(tc.target), len(tc.unresolved)+1)
			}
		})
	}
}

// givingUpResolver is the resolver of a request whose client gives up while
// the scope check waits for it, a closed page or the frontend's own timeout:
// asked, it ends the request's context and answers once that is done. With
// giveUp nil it answers a SERVFAIL at once, for a client still there.
type givingUpResolver struct {
	mu     sync.Mutex
	giveUp func()
}

func (r *givingUpResolver) LookupNetIP(ctx context.Context, _, _ string) ([]netip.Addr, error) {
	r.mu.Lock()
	giveUp := r.giveUp
	r.mu.Unlock()
	if giveUp == nil {
		return nil, servfail
	}
	giveUp()
	<-ctx.Done()
	return nil, ctx.Err()
}

// A lookup the client gave up on is counted as resolve_error, a user's, and
// leaves no deny row: it ended with the request, not because of where the
// name leads. The response is the 403 any refusal gets, byte for byte, and a
// lookup that fails for a client still there leaves its row.
func TestScopeDenialClientGaveUp(t *testing.T) {
	resolver := &givingUpResolver{}
	meter := metrics.New("test")
	srv, db := newTestAPIWith(t, func(o *Options) { o.Resolver, o.Meter = resolver, meter })
	token := signInScoped(t, srv.URL)
	create := func(ctx context.Context, name string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/monitors",
			strings.NewReader(monitorJSON(name, "http", "https://status.vendor.example/", `"namespace":"shop"`))).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.Config.Handler.ServeHTTP(rec, req)
		return rec
	}

	ctx, giveUp := context.WithCancel(context.Background())
	defer giveUp()
	resolver.mu.Lock()
	resolver.giveUp = giveUp
	resolver.mu.Unlock()
	gaveUp := create(ctx, "gave-up")

	resolver.mu.Lock()
	resolver.giveUp = nil
	resolver.mu.Unlock()
	stayed := create(context.Background(), "stayed")

	for name, rec := range map[string]*httptest.ResponseRecorder{"client gave up": gaveUp, "client still there": stayed} {
		var body errorBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusForbidden || body != (errorBody{Error: scopeRefused, Code: codeTargetScope}) {
			t.Errorf("%s: %d %s, want 403 with the generic refusal", name, rec.Code, rec.Body)
		}
	}
	if gaveUp.Body.String() != stayed.Body.String() {
		t.Errorf("the responses differ: %s and %s", gaveUp.Body, stayed.Body)
	}
	got, err := meter.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if n := got[scopeDeniedKey(metrics.ScopeDeniedResolveError)]; n != 2 {
		t.Errorf("reason=resolve_error = %v, want 2", n)
	}
	if n := got[scopeDeniedKey(metrics.ScopeDeniedTarget)]; n != 0 {
		t.Errorf("reason=target = %v, want 0", n)
	}
	rows := auditDenials(t, db)
	if len(rows) != 1 || rows[0].Summary != "target status.vendor.example did not resolve, so it could not be shown to be outside the cluster" {
		t.Errorf("deny rows = %+v, want the one of the client still there", rows)
	}
	if monitors, err := db.ListMonitors(context.Background()); err != nil || len(monitors) != 0 {
		t.Errorf("monitors = %v, %v; want none", monitors, err)
	}
}
