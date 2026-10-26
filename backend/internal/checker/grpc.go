package checker

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/yusufceng/Kubeat/backend/internal/explain"
	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// checkGRPC asks the target's health service (grpc.health.v1.Health, the
// protocol Kubernetes gRPC probes use) whether it is serving. The outcome
// reports the connections made, at the address each reached.
func (c *Checker) checkGRPC(ctx context.Context, m store.Monitor) outcome {
	d := c.dials(m)
	return d.made(c.grpcHealth(ctx, m, d))
}

func (c *Checker) grpcHealth(ctx context.Context, m store.Monitor, d *dials) outcome {
	transport := insecure.NewCredentials()
	if m.GRPC.TLS {
		host, _, err := net.SplitHostPort(m.Target)
		if err != nil {
			return failed(err)
		}
		transport = credentials.NewTLS(c.tlsConfig(host, m.GRPC.SkipTLSVerify))
	}

	// passthrough: the address is dialled as given, without gRPC's own name
	// resolution and its extra lookups.
	dial := d.dial(c)
	conn, err := grpc.NewClient("passthrough:///"+m.Target,
		grpc.WithTransportCredentials(transport),
		grpc.WithUserAgent(c.opts.UserAgent),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return dial(ctx, "tcp", addr)
		}))
	if err != nil {
		return failed(err)
	}
	defer conn.Close()

	resp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{Service: m.GRPC.Service})
	if err != nil {
		st := status.Convert(err)
		switch st.Code() {
		case codes.Unimplemented:
			return failedf("the server does not implement the gRPC health checking protocol")
		case codes.NotFound:
			return failedf("the server does not know the service %q", m.GRPC.Service)
		case codes.DeadlineExceeded:
			return failed(context.DeadlineExceeded)
		}
		out := failed(d.wrap(fmt.Errorf("%s: %s", st.Code(), st.Message())))
		// A status carries the transport error behind it, or what a proxy in
		// front of the server said, as text only.
		if e := explain.Message(st.Message()); e != nil {
			out.diag = &store.Diagnostics{Explanation: e}
		}
		return out
	}
	if resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		return failedf("health status is %s", resp.GetStatus())
	}
	return outcome{detail: resp.GetStatus().String()}
}
