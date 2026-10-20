package api

import (
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/explain"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// diagnosticsResponse is why a failed check failed, as far as the check
// could tell (store.Diagnostics). Every field is present; what the check
// did not learn is null.
type diagnosticsResponse struct {
	// Explanation is what the failure most likely means; null when no rule
	// recognized it.
	Explanation *explanationResponse `json:"explanation"`
	// Timing is where the time of an HTTP(S) or WebSocket check went; null
	// for the other types.
	Timing *timingResponse `json:"timing"`
	// RemoteAddr is the address connected to ("ip:port"), or the last one
	// tried when no connection was made.
	RemoteAddr *string      `json:"remote_addr"`
	TLS        *tlsResponse `json:"tls"`
	// StatusLine is the status line of the answer that was judged, such as
	// "HTTP/1.1 503 Service Unavailable"; null when none came.
	StatusLine *string `json:"status_line"`
	// Headers are the few headers of that answer that are kept, by
	// lower-case name; empty when none.
	Headers map[string]string `json:"headers"`
	// Redirects is how many redirects were followed before that answer.
	Redirects int `json:"redirects"`
	// Pod is the pod these details are about, for a monitor that checks
	// each pod behind its service.
	Pod *string `json:"pod"`
}

// explanationResponse is what a failure most likely means: a stable code
// and the values its wording needs, which the frontend words in each user's
// language, and Text, the English wording, for a client that does not know
// the code. A code from a later version has an empty text.
type explanationResponse struct {
	Code   string            `json:"code"`
	Params map[string]string `json:"params"`
	Text   string            `json:"text"`
}

// timingResponse is in milliseconds; a phase that did not happen is null.
type timingResponse struct {
	DNSMS     *float64 `json:"dns_ms"`
	ConnectMS *float64 `json:"connect_ms"`
	TLSMS     *float64 `json:"tls_ms"`
	// FirstByteMS is from the start of the request to the first byte of
	// its answer, the phases above included.
	FirstByteMS *float64 `json:"first_byte_ms"`
	// TotalMS is the whole check, redirects included.
	TotalMS float64 `json:"total_ms"`
}

// tlsResponse describes the TLS connection and the certificate the server
// presented, also one that failed verification.
type tlsResponse struct {
	// Version is null when the handshake failed before one was agreed.
	Version *string `json:"version"`
	// Subject and Issuer are common names; null when the certificate has
	// none, or none was seen.
	Subject *string `json:"subject"`
	Issuer  *string `json:"issuer"`
	// SANs are the first names and addresses the certificate is valid
	// for; MoreSANs counts the others.
	SANs     []string   `json:"sans"`
	MoreSANs int        `json:"more_sans"`
	NotAfter *time.Time `json:"not_after"`
}

func toDiagnosticsResponse(d store.Diagnostics) *diagnosticsResponse {
	resp := &diagnosticsResponse{
		RemoteAddr: nullable(d.RemoteAddr),
		StatusLine: nullable(d.StatusLine),
		Headers:    d.Headers,
		Redirects:  d.Redirects,
		Pod:        nullable(d.Pod),
	}
	if resp.Headers == nil {
		resp.Headers = map[string]string{}
	}
	if e := d.Explanation; e != nil {
		resp.Explanation = &explanationResponse{Code: e.Code, Params: e.Params, Text: explain.Text(*e, explain.English)}
		if resp.Explanation.Params == nil {
			resp.Explanation.Params = map[string]string{}
		}
	}
	if t := d.Timing; t != nil {
		resp.Timing = &timingResponse{DNSMS: t.DNS, ConnectMS: t.Connect, TLSMS: t.TLS, FirstByteMS: t.FirstByte, TotalMS: t.Total}
	}
	if t := d.TLS; t != nil {
		resp.TLS = &tlsResponse{
			Version: nullable(t.Version), Subject: nullable(t.Subject), Issuer: nullable(t.Issuer),
			SANs: t.SANs, MoreSANs: t.MoreSANs,
		}
		if resp.TLS.SANs == nil {
			resp.TLS.SANs = []string{}
		}
		if !t.NotAfter.IsZero() {
			notAfter := t.NotAfter
			resp.TLS.NotAfter = &notAfter
		}
	}
	return resp
}

// nullable is s, or nil when it is empty.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
