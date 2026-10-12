package checker

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// ICMP protocol numbers, as icmp.ParseMessage wants them.
const (
	protocolICMPv4 = 1
	protocolICMPv6 = 58
)

// pingPayload is the size of the data sent with an echo request, the size
// the ping command uses.
const pingPayload = 56

// checkPing sends one ICMP echo request and waits for its reply. The
// outcome's latency is the round-trip time, without the name lookup.
//
// It uses an unprivileged ICMP socket, which needs no capability but must be
// allowed by the kernel (net.ipv4.ping_group_range on Linux); container
// runtimes allow it by default.
func (c *Checker) checkPing(ctx context.Context, host string) outcome {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return failed(err)
	}
	ip := addrs[0].IP
	for _, a := range addrs { // prefer IPv4, like most tools do
		if a.IP.To4() != nil {
			ip = a.IP
			break
		}
	}

	network, listen, protocol := "udp4", "0.0.0.0", protocolICMPv4
	var request icmp.Type = ipv4.ICMPTypeEcho
	var reply icmp.Type = ipv4.ICMPTypeEchoReply
	if ip.To4() == nil {
		network, listen, protocol = "udp6", "::", protocolICMPv6
		request, reply = ipv6.ICMPTypeEchoRequest, ipv6.ICMPTypeEchoReply
	}

	conn, err := icmp.ListenPacket(network, listen)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return failedf("ping is not permitted here: the kernel does not allow unprivileged ICMP sockets (net.ipv4.ping_group_range)")
		}
		return failed(err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	// The kernel may rewrite the echo identifier of an unprivileged socket,
	// so the reply is recognized by its random payload instead.
	payload := make([]byte, pingPayload)
	_, _ = rand.Read(payload)
	packet, err := (&icmp.Message{Type: request, Body: &icmp.Echo{Seq: 1, Data: payload}}).Marshal(nil)
	if err != nil {
		return failed(err)
	}

	start := time.Now()
	if _, err := conn.WriteTo(packet, &net.UDPAddr{IP: ip}); err != nil {
		return failed(err)
	}
	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return failedf("no reply from %s", ip)
			}
			return failed(err)
		}
		rtt := time.Since(start)
		message, err := icmp.ParseMessage(protocol, buf[:n])
		if err != nil {
			continue
		}
		echo, ok := message.Body.(*icmp.Echo)
		if message.Type != reply || !ok || !bytes.Equal(echo.Data, payload) {
			continue // something else addressed to this socket
		}
		return outcome{detail: fmt.Sprintf("reply from %s", ip), latency: rtt, hasTime: true}
	}
}
