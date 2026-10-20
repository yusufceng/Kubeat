package ldapauth

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// connect opens a connection to the directory, over TLS from the start for
// ldaps:// or after StartTLS when that is asked for. The connection lasts
// no longer than ctx: whatever it waits for, it is closed once ctx is
// done, so a sign-in whose client went away stops at once instead of after
// the directory's timeout. go-ldap dials without a context, so the dial is
// done here. Each step is bounded by the configured timeout besides. The
// returned function stops watching ctx; call it when done.
func (c *Client) connect(ctx context.Context) (*ldap.Conn, func() bool, error) {
	dialCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	raw, err := new(net.Dialer).DialContext(dialCtx, "tcp", c.address)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	var transport net.Conn = raw
	if c.ldaps {
		tlsConn := tls.Client(raw, c.tls)
		if err := tlsConn.HandshakeContext(dialCtx); err != nil {
			stop()
			raw.Close()
			return nil, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		transport = tlsConn
	}
	conn := ldap.NewConn(transport, c.ldaps)
	conn.Start()
	conn.SetTimeout(c.cfg.Timeout)
	if c.cfg.StartTLS && !c.ldaps {
		// go-ldap shakes hands without a deadline: a directory that agrees
		// to StartTLS and then says nothing would hold the connection for
		// as long as ctx lasts.
		_ = raw.SetDeadline(time.Now().Add(c.cfg.Timeout))
		err := conn.StartTLS(c.tls)
		_ = raw.SetDeadline(time.Time{})
		if err != nil {
			conn.Close()
			stop()
			return nil, nil, fmt.Errorf("%w: StartTLS: %v", ErrUnavailable, err)
		}
	}
	return conn, stop, nil
}
