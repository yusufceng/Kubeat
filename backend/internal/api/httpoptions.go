package api

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/yusufceng/Kubeat/backend/internal/checker"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

const (
	maxHeaders       = 20
	maxHeaderValue   = 4096
	maxKeywordLength = 1024
	// maskedValue stands in for a header value in responses: header values
	// are often credentials. Sent back unchanged in an update, it means
	// "keep the stored value".
	maskedValue = "********"
)

var headerName = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+.^_|~-]+$`)

// httpOptions is the JSON form of a monitor's HTTP check options.
type httpOptions struct {
	Method         string            `json:"method"`
	ExpectedStatus string            `json:"expected_status"`
	Keyword        string            `json:"keyword"`
	Headers        map[string]string `json:"headers"`
	TLSSkipVerify  bool              `json:"tls_skip_verify"`
}

func toHTTPOptions(o store.HTTPOptions) *httpOptions {
	resp := &httpOptions{
		Method:         o.Method,
		ExpectedStatus: o.ExpectedStatus,
		Keyword:        o.Keyword,
		Headers:        make(map[string]string, len(o.Headers)),
		TLSSkipVerify:  o.SkipTLSVerify,
	}
	for name := range o.Headers {
		resp.Headers[name] = maskedValue
	}
	return resp
}

// resolve validates the options and returns their stored form. existing
// holds the monitor's current headers (nil for a new monitor), from which
// masked values are filled in.
func (o *httpOptions) resolve(existing map[string]string) (store.HTTPOptions, error) {
	out := store.HTTPOptions{
		Method:         strings.ToUpper(strings.TrimSpace(o.Method)),
		ExpectedStatus: strings.TrimSpace(o.ExpectedStatus),
		Keyword:        o.Keyword,
		SkipTLSVerify:  o.TLSSkipVerify,
	}
	if out.Method == "" {
		out.Method = store.DefaultHTTPMethod
	}
	if out.Method != http.MethodGet && out.Method != http.MethodHead {
		return store.HTTPOptions{}, errors.New("http.method must be GET or HEAD")
	}
	if out.ExpectedStatus == "" {
		out.ExpectedStatus = store.DefaultExpectedStatus
	}
	if _, err := checker.ParseStatusSet(out.ExpectedStatus); err != nil {
		return store.HTTPOptions{}, fmt.Errorf("http.expected_status: %w", err)
	}
	if len(out.Keyword) > maxKeywordLength {
		return store.HTTPOptions{}, fmt.Errorf("http.keyword must be at most %d bytes", maxKeywordLength)
	}
	if out.Keyword != "" && out.Method == http.MethodHead {
		return store.HTTPOptions{}, errors.New("http.keyword needs the GET method: a HEAD response has no body")
	}

	if len(o.Headers) > maxHeaders {
		return store.HTTPOptions{}, fmt.Errorf("http.headers: at most %d headers", maxHeaders)
	}
	for name, value := range o.Headers {
		if !headerName.MatchString(name) {
			return store.HTTPOptions{}, fmt.Errorf("http.headers: %q is not a valid header name", name)
		}
		name = http.CanonicalHeaderKey(name)
		if value == maskedValue {
			stored, ok := existing[name]
			if !ok {
				return store.HTTPOptions{}, fmt.Errorf("http.headers: %s has no stored value; enter one", name)
			}
			value = stored
		}
		if len(value) > maxHeaderValue || strings.ContainsAny(value, "\r\n") {
			return store.HTTPOptions{}, fmt.Errorf("http.headers: the value of %s is too long or contains a line break", name)
		}
		if out.Headers == nil {
			out.Headers = make(map[string]string, len(o.Headers))
		}
		if _, dup := out.Headers[name]; dup {
			return store.HTTPOptions{}, fmt.Errorf("http.headers: %s is given more than once", name)
		}
		out.Headers[name] = value
	}
	return out, nil
}
