package checker

import (
	"context"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// checkGRPC asks the target's health service (grpc.health.v1.Health, the
// protocol Kubernetes gRPC probes use) whether it is serving.
func (c *Checker) checkGRPC(ctx context.Context, m store.Monitor) outcome {
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
	conn, err := grpc.NewClient("passthrough:///"+m.Target,
		grpc.WithTransportCredentials(transport),
		grpc.WithUserAgent(c.opts.UserAgent),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return c.dialer.DialContext(ctx, "tcp", addr)
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
		return failedf("%s: %s", st.Code(), st.Message())
	}
	if resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		return failedf("health status is %s", resp.GetStatus())
	}
	return outcome{detail: resp.GetStatus().String()}
}
