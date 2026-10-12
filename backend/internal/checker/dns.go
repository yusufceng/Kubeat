package checker

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// DNSRecordTypes are the record types a DNS monitor can look up.
var DNSRecordTypes = []string{"A", "AAAA", "CNAME", "MX", "NS", "TXT", "SRV", "PTR"}

// defaultDNSPort is used when a monitor's resolver names no port.
const defaultDNSPort = "53"

// resolver returns the resolver for a monitor: the system's, or one that
// asks only the given server.
func (c *Checker) resolver(server string) *net.Resolver {
	if server == "" {
		return net.DefaultResolver
	}
	if _, _, err := net.SplitHostPort(server); err != nil {
		server = net.JoinHostPort(server, defaultDNSPort)
	}
	return &net.Resolver{
		// The Go resolver is the one that honours Dial.
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return c.dialer.DialContext(ctx, network, server)
		},
	}
}

// checkDNS looks up the monitor's target. It succeeds when at least one
// record comes back and, if the monitor expects a value, one contains it.
func (c *Checker) checkDNS(ctx context.Context, m store.Monitor) outcome {
	records, err := lookup(ctx, c.resolver(m.DNS.Resolver), m.DNS.RecordType, m.Target)
	if err != nil {
		return failed(err)
	}
	if len(records) == 0 {
		return failedf("no %s records for %s", m.DNS.RecordType, m.Target)
	}
	sort.Strings(records)
	found := strings.Join(records, ", ")

	if want := m.DNS.Expected; want != "" {
		matched := false
		for _, record := range records {
			matched = matched || strings.Contains(strings.ToLower(record), strings.ToLower(want))
		}
		if !matched {
			return failedf("no %s record contains %q; found %s", m.DNS.RecordType, want, truncate(found, maxDetail))
		}
	}
	return outcome{detail: found}
}

// lookup returns the records of one type as text, without trailing dots.
func lookup(ctx context.Context, r *net.Resolver, recordType, name string) ([]string, error) {
	host := func(s string) string { return strings.TrimSuffix(s, ".") }
	var records []string

	switch recordType {
	case "A", "AAAA":
		network := "ip4"
		if recordType == "AAAA" {
			network = "ip6"
		}
		ips, err := r.LookupIP(ctx, network, name)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			records = append(records, ip.String())
		}
	case "CNAME":
		cname, err := r.LookupCNAME(ctx, name)
		if err != nil {
			return nil, err
		}
		// The resolver answers with the name itself when it has no alias.
		if host(cname) != host(name) {
			records = append(records, host(cname))
		}
	case "MX":
		mxs, err := r.LookupMX(ctx, name)
		if err != nil {
			return nil, err
		}
		for _, mx := range mxs {
			records = append(records, fmt.Sprintf("%d %s", mx.Pref, host(mx.Host)))
		}
	case "NS":
		nss, err := r.LookupNS(ctx, name)
		if err != nil {
			return nil, err
		}
		for _, ns := range nss {
			records = append(records, host(ns.Host))
		}
	case "TXT":
		txts, err := r.LookupTXT(ctx, name)
		if err != nil {
			return nil, err
		}
		records = txts
	case "SRV":
		// The whole name is given, e.g. _https._tcp.example.com.
		_, srvs, err := r.LookupSRV(ctx, "", "", name)
		if err != nil {
			return nil, err
		}
		for _, srv := range srvs {
			records = append(records, fmt.Sprintf("%s:%d", host(srv.Target), srv.Port))
		}
	case "PTR":
		names, err := r.LookupAddr(ctx, name)
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			records = append(records, host(n))
		}
	default:
		return nil, fmt.Errorf("unsupported record type %q", recordType)
	}
	return records, nil
}
