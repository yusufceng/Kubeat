package api

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/yusufceng/Kubeat/backend/internal/checker"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

const (
	maxHeaders       = 20
	maxHeaderValue   = 4096
	maxKeywordLength = 1024
	maxBodyLength    = 64 << 10
	maxOptionLength  = 255
	maxRedirects     = 20
	// maskedValue stands in for a secret in responses: header values and
	// the password or token of an HTTP check. Sent back unchanged in an
	// update, it means "keep the stored value".
	maskedValue = "********"
)

var headerName = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+.^_|~-]+$`)

// checkOptions is the JSON form of a monitor's check options: one object
// per monitor type, of which a monitor has only its own.
type checkOptions struct {
	HTTP      *httpOptions      `json:"http"`
	TCP       *tcpOptions       `json:"tcp"`
	DNS       *dnsOptions       `json:"dns"`
	GRPC      *grpcOptions      `json:"grpc"`
	WebSocket *websocketOptions `json:"websocket"`
	// Database belongs to the postgres, mysql and redis types.
	Database *databaseOptions `json:"database"`
	Workload *workloadOptions `json:"workload"`
}

type workloadOptions struct {
	// MinReady is how many pods must be ready; 0 means all of them.
	MinReady int `json:"min_ready"`
}

// maxMinReady bounds the pods a workload monitor may ask for.
const maxMinReady = 10000

type httpOptions struct {
	Method         string `json:"method"`
	ExpectedStatus string `json:"expected_status"`
	Keyword        string `json:"keyword"`
	InvertKeyword  bool   `json:"invert_keyword"`
	JSONPath       string `json:"json_path"`
	JSONOperator   string `json:"json_operator"`
	JSONExpected   string `json:"json_expected"`
	// Header values come back masked.
	Headers     map[string]string `json:"headers"`
	Body        string            `json:"body"`
	ContentType string            `json:"content_type"`
	// Auth is "", "basic" or "bearer"; AuthSecret comes back masked.
	Auth       string `json:"auth"`
	AuthUser   string `json:"auth_user"`
	AuthSecret string `json:"auth_secret"`
	// MaxRedirects defaults to store.DefaultMaxRedirects when omitted.
	MaxRedirects  *int `json:"max_redirects"`
	TLSSkipVerify bool `json:"tls_skip_verify"`
}

type tcpOptions struct {
	TLS           bool `json:"tls"`
	TLSSkipVerify bool `json:"tls_skip_verify"`
}

type dnsOptions struct {
	RecordType string `json:"record_type"`
	Resolver   string `json:"resolver"`
	Expected   string `json:"expected"`
}

type grpcOptions struct {
	Service       string `json:"service"`
	TLS           bool   `json:"tls"`
	TLSSkipVerify bool   `json:"tls_skip_verify"`
}

type websocketOptions struct {
	Subprotocol   string `json:"subprotocol"`
	TLSSkipVerify bool   `json:"tls_skip_verify"`
}

type databaseOptions struct {
	Username string `json:"username"`
	// Password comes back masked.
	Password string `json:"password"`
	// Database and Query do not apply to Redis.
	Database      string `json:"database"`
	Query         string `json:"query"`
	TLS           bool   `json:"tls"`
	TLSSkipVerify bool   `json:"tls_skip_verify"`
}

const maxQueryLength = 1000

// toCheckOptions returns the options of the monitor's type, with secrets
// masked.
func toCheckOptions(m store.Monitor) checkOptions {
	var o checkOptions
	switch m.Type {
	case store.MonitorHTTP:
		h := m.HTTP
		redirects := h.MaxRedirects
		o.HTTP = &httpOptions{
			Method: h.Method, ExpectedStatus: h.ExpectedStatus,
			Keyword: h.Keyword, InvertKeyword: h.InvertKeyword,
			JSONPath: h.JSONPath, JSONOperator: h.JSONOperator, JSONExpected: h.JSONExpected,
			Headers: make(map[string]string, len(h.Headers)),
			Body:    h.Body, ContentType: h.ContentType,
			Auth: string(h.Auth), AuthUser: h.AuthUser,
			MaxRedirects: &redirects, TLSSkipVerify: h.SkipTLSVerify,
		}
		for name := range h.Headers {
			o.HTTP.Headers[name] = maskedValue
		}
		if h.AuthSecret != "" {
			o.HTTP.AuthSecret = maskedValue
		}
	case store.MonitorTCP:
		o.TCP = &tcpOptions{TLS: m.TCP.TLS, TLSSkipVerify: m.TCP.SkipTLSVerify}
	case store.MonitorDNS:
		o.DNS = &dnsOptions{RecordType: m.DNS.RecordType, Resolver: m.DNS.Resolver, Expected: m.DNS.Expected}
	case store.MonitorGRPC:
		o.GRPC = &grpcOptions{Service: m.GRPC.Service, TLS: m.GRPC.TLS, TLSSkipVerify: m.GRPC.SkipTLSVerify}
	case store.MonitorWebSocket:
		o.WebSocket = &websocketOptions{Subprotocol: m.WebSocket.Subprotocol, TLSSkipVerify: m.WebSocket.SkipTLSVerify}
	case store.MonitorPostgres, store.MonitorMySQL, store.MonitorRedis:
		d := m.Database
		o.Database = &databaseOptions{
			Username: d.Username, Database: d.Database, Query: d.Query, TLS: d.TLS, TLSSkipVerify: d.SkipTLSVerify,
		}
		if d.Password != "" {
			o.Database.Password = maskedValue
		}
	case store.MonitorWorkload:
		o.Workload = &workloadOptions{MinReady: m.Workload.MinReady}
	}
	return o
}

// defaultOptions returns the check options of a new monitor of the given
// type that names none.
func defaultOptions(typ store.MonitorType) store.Monitor {
	var m store.Monitor
	switch typ {
	case store.MonitorHTTP:
		m.HTTP = store.HTTPOptions{
			Method: store.DefaultHTTPMethod, ExpectedStatus: store.DefaultExpectedStatus, MaxRedirects: store.DefaultMaxRedirects,
		}
	case store.MonitorPostgres, store.MonitorMySQL:
		m.Database.Query = store.DefaultDatabaseQuery
	}
	return m
}

// apply validates the options and sets them on m. The options of m's type
// replace what m has as a whole; options of another type are an error.
// Masked secrets are filled in from what m holds.
func (o checkOptions) apply(m *store.Monitor) error {
	// The block each type owns; the database types share one.
	own := string(m.Type)
	if m.Type.IsDatabase() {
		own = "database"
	}
	for name, present := range map[string]bool{
		"http": o.HTTP != nil, "tcp": o.TCP != nil, "dns": o.DNS != nil,
		"grpc": o.GRPC != nil, "websocket": o.WebSocket != nil, "database": o.Database != nil,
		"workload": o.Workload != nil,
	} {
		if present && name != own {
			return fmt.Errorf("%s options are not valid for a %s monitor", name, m.Type)
		}
	}

	var err error
	switch {
	case o.HTTP != nil:
		m.HTTP, err = o.HTTP.resolve(m.HTTP)
	case o.TCP != nil:
		m.TCP = store.TCPOptions{TLS: o.TCP.TLS, SkipTLSVerify: o.TCP.TLS && o.TCP.TLSSkipVerify}
	case o.DNS != nil:
		m.DNS, err = o.DNS.resolve()
	case o.GRPC != nil:
		m.GRPC, err = o.GRPC.resolve()
	case o.WebSocket != nil:
		m.WebSocket, err = o.WebSocket.resolve()
	case o.Database != nil:
		m.Database, err = o.Database.resolve(m.Type, m.Database)
	case o.Workload != nil:
		if o.Workload.MinReady < 0 || o.Workload.MinReady > maxMinReady {
			return fmt.Errorf("workload.min_ready must be between 0 and %d", maxMinReady)
		}
		m.Workload = store.WorkloadOptions{MinReady: o.Workload.MinReady}
	}
	return err
}

// resolve validates the options and returns their stored form. existing
// holds the monitor's current options, from which a masked password is
// filled in.
func (o *databaseOptions) resolve(typ store.MonitorType, existing store.DatabaseOptions) (store.DatabaseOptions, error) {
	none := store.DatabaseOptions{}
	out := store.DatabaseOptions{
		Username: strings.TrimSpace(o.Username), Password: o.Password,
		TLS: o.TLS, SkipTLSVerify: o.TLS && o.TLSSkipVerify,
	}
	if out.Password == maskedValue {
		if existing.Password == "" {
			return none, errors.New("database.password has no stored value; enter one")
		}
		out.Password = existing.Password
	}
	if !singleLine(out.Username) || len(out.Password) > maxHeaderValue || strings.ContainsAny(out.Password, "\r\n") {
		return none, errors.New("database.username or database.password is too long or contains a line break")
	}

	if typ == store.MonitorRedis {
		if strings.TrimSpace(o.Database) != "" || strings.TrimSpace(o.Query) != "" {
			return none, errors.New("database.database and database.query do not apply to redis: the check is a PING")
		}
		if out.Username != "" && out.Password == "" {
			return none, errors.New("database.password is required with a username")
		}
		return out, nil
	}

	out.Database, out.Query = strings.TrimSpace(o.Database), strings.TrimSpace(o.Query)
	if out.Query == "" {
		out.Query = store.DefaultDatabaseQuery
	}
	switch {
	case out.Username == "":
		return none, errors.New("database.username is required")
	case !singleLine(out.Database):
		return none, errors.New("database.database is too long or contains a line break")
	case len(out.Query) > maxQueryLength:
		return none, fmt.Errorf("database.query must be at most %d bytes", maxQueryLength)
	}
	return out, nil
}

// singleLine reports whether s fits an option that ends up in a header or
// a protocol field.
func singleLine(s string) bool {
	return len(s) <= maxOptionLength && !strings.ContainsAny(s, "\r\n")
}

// resolve validates the options and returns their stored form. existing
// holds the monitor's current options (zero for a new monitor), from which
// masked secrets are filled in.
func (o *httpOptions) resolve(existing store.HTTPOptions) (store.HTTPOptions, error) {
	none := store.HTTPOptions{}
	out := store.HTTPOptions{
		Method:         strings.ToUpper(strings.TrimSpace(o.Method)),
		ExpectedStatus: strings.TrimSpace(o.ExpectedStatus),
		Keyword:        o.Keyword,
		InvertKeyword:  o.InvertKeyword && o.Keyword != "",
		JSONPath:       strings.TrimSpace(o.JSONPath),
		Body:           o.Body,
		Auth:           store.HTTPAuth(o.Auth),
		MaxRedirects:   store.DefaultMaxRedirects,
		SkipTLSVerify:  o.TLSSkipVerify,
	}
	if out.Method == "" {
		out.Method = store.DefaultHTTPMethod
	}
	if !slices.Contains(checker.HTTPMethods, out.Method) {
		return none, fmt.Errorf("http.method must be one of %s", strings.Join(checker.HTTPMethods, ", "))
	}
	if out.ExpectedStatus == "" {
		out.ExpectedStatus = store.DefaultExpectedStatus
	}
	if _, err := checker.ParseStatusSet(out.ExpectedStatus); err != nil {
		return none, fmt.Errorf("http.expected_status: %w", err)
	}
	if o.MaxRedirects != nil {
		if *o.MaxRedirects < 0 || *o.MaxRedirects > maxRedirects {
			return none, fmt.Errorf("http.max_redirects must be between 0 and %d", maxRedirects)
		}
		out.MaxRedirects = *o.MaxRedirects
	}

	if len(out.Keyword) > maxKeywordLength {
		return none, fmt.Errorf("http.keyword must be at most %d bytes", maxKeywordLength)
	}
	if out.JSONPath != "" {
		if _, err := checker.ParseJSONPath(out.JSONPath); err != nil || len(out.JSONPath) > maxOptionLength {
			return none, errors.New(`http.json_path must be a path such as "status" or "checks[0].state"`)
		}
		out.JSONOperator, out.JSONExpected = o.JSONOperator, o.JSONExpected
		if out.JSONOperator == "" {
			out.JSONOperator = checker.DefaultJSONOperator
		}
		if !slices.Contains(checker.JSONOperators, out.JSONOperator) {
			return none, fmt.Errorf("http.json_operator must be one of %s", strings.Join(checker.JSONOperators, " "))
		}
		if len(out.JSONExpected) > maxKeywordLength {
			return none, fmt.Errorf("http.json_expected must be at most %d bytes", maxKeywordLength)
		}
	}
	if (out.Keyword != "" || out.JSONPath != "") && out.Method == http.MethodHead {
		return none, errors.New("http.keyword and http.json_path need a method other than HEAD: a HEAD response has no body")
	}

	if len(out.Body) > maxBodyLength {
		return none, fmt.Errorf("http.body must be at most %d bytes", maxBodyLength)
	}
	if out.Body != "" {
		out.ContentType = strings.TrimSpace(o.ContentType)
		if !singleLine(out.ContentType) {
			return none, errors.New("http.content_type is too long or contains a line break")
		}
	}

	switch out.Auth {
	case store.HTTPAuthNone:
	case store.HTTPAuthBasic, store.HTTPAuthBearer:
		out.AuthSecret = o.AuthSecret
		if out.AuthSecret == maskedValue {
			// Only the secret of the same kind of credential is kept.
			if existing.Auth != out.Auth || existing.AuthSecret == "" {
				return none, errors.New("http.auth_secret has no stored value; enter one")
			}
			out.AuthSecret = existing.AuthSecret
		}
		if out.Auth == store.HTTPAuthBasic {
			out.AuthUser = o.AuthUser
			if out.AuthUser == "" || strings.Contains(out.AuthUser, ":") {
				return none, errors.New("http.auth_user is required for basic authentication and must not contain a colon")
			}
		} else if out.AuthSecret == "" {
			return none, errors.New("http.auth_secret is required for bearer authentication")
		}
		if len(out.AuthUser) > maxOptionLength || len(out.AuthSecret) > maxHeaderValue || strings.ContainsAny(out.AuthUser+out.AuthSecret, "\r\n") {
			return none, errors.New("http.auth_user or http.auth_secret is too long or contains a line break")
		}
	default:
		return none, errors.New(`http.auth must be "basic", "bearer" or empty`)
	}

	if len(o.Headers) > maxHeaders {
		return none, fmt.Errorf("http.headers: at most %d headers", maxHeaders)
	}
	for name, value := range o.Headers {
		if !headerName.MatchString(name) {
			return none, fmt.Errorf("http.headers: %q is not a valid header name", name)
		}
		name = http.CanonicalHeaderKey(name)
		if value == maskedValue {
			stored, ok := existing.Headers[name]
			if !ok {
				return none, fmt.Errorf("http.headers: %s has no stored value; enter one", name)
			}
			value = stored
		}
		if len(value) > maxHeaderValue || strings.ContainsAny(value, "\r\n") {
			return none, fmt.Errorf("http.headers: the value of %s is too long or contains a line break", name)
		}
		if out.Headers == nil {
			out.Headers = make(map[string]string, len(o.Headers))
		}
		if _, dup := out.Headers[name]; dup {
			return none, fmt.Errorf("http.headers: %s is given more than once", name)
		}
		out.Headers[name] = value
	}
	return out, nil
}

func (o *dnsOptions) resolve() (store.DNSOptions, error) {
	out := store.DNSOptions{
		RecordType: strings.ToUpper(strings.TrimSpace(o.RecordType)),
		Resolver:   strings.TrimSpace(o.Resolver),
		Expected:   strings.TrimSpace(o.Expected),
	}
	if out.RecordType == "" {
		out.RecordType = store.DefaultDNSRecordType
	}
	if !slices.Contains(checker.DNSRecordTypes, out.RecordType) {
		return store.DNSOptions{}, fmt.Errorf("dns.record_type must be one of %s", strings.Join(checker.DNSRecordTypes, ", "))
	}
	if out.Resolver != "" {
		host := out.Resolver
		if h, port, err := net.SplitHostPort(out.Resolver); err == nil {
			if !validPort(port) {
				return store.DNSOptions{}, errors.New("dns.resolver: the port must be between 1 and 65535")
			}
			host = h
		}
		if !validHost(host) {
			return store.DNSOptions{}, errors.New("dns.resolver must be a host or host:port")
		}
	}
	if !singleLine(out.Expected) {
		return store.DNSOptions{}, errors.New("dns.expected is too long or contains a line break")
	}
	return out, nil
}

func (o *grpcOptions) resolve() (store.GRPCOptions, error) {
	out := store.GRPCOptions{Service: strings.TrimSpace(o.Service), TLS: o.TLS, SkipTLSVerify: o.TLS && o.TLSSkipVerify}
	if !singleLine(out.Service) {
		return store.GRPCOptions{}, errors.New("grpc.service is too long or contains a line break")
	}
	return out, nil
}

func (o *websocketOptions) resolve() (store.WebSocketOptions, error) {
	out := store.WebSocketOptions{Subprotocol: strings.TrimSpace(o.Subprotocol), SkipTLSVerify: o.TLSSkipVerify}
	// A subprotocol is a token, like a header name.
	if out.Subprotocol != "" && (!headerName.MatchString(out.Subprotocol) || len(out.Subprotocol) > maxOptionLength) {
		return store.WebSocketOptions{}, errors.New("websocket.subprotocol is not a valid subprotocol name")
	}
	return out, nil
}
