package ldapauth

import (
	"context"
	"net"
)

// dialerFor returns a dialer bound to ctx's deadline.
func dialerFor(ctx context.Context) *net.Dialer {
	d := &net.Dialer{}
	if deadline, ok := ctx.Deadline(); ok {
		d.Deadline = deadline
	}
	return d
}
