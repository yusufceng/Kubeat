package checker

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// fakeRedis speaks enough of the Redis protocol for a login and a PING.
// password, when set, must be given first; user is the name it belongs to.
func fakeRedis(t *testing.T, user, password, pong string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				reader := bufio.NewReader(conn)
				authed := password == ""
				for {
					args, err := readCommand(reader)
					if err != nil {
						return
					}
					switch strings.ToUpper(args[0]) {
					case "AUTH":
						given := args[1:]
						ok := len(given) == 1 && user == "" && given[0] == password ||
							len(given) == 2 && given[0] == user && given[1] == password
						if !ok {
							conn.Write([]byte("-WRONGPASS invalid username-password pair or user is disabled.\r\n"))
							continue
						}
						authed = true
						conn.Write([]byte("+OK\r\n"))
					case "PING":
						if !authed {
							conn.Write([]byte("-NOAUTH Authentication required.\r\n"))
							continue
						}
						conn.Write([]byte(pong + "\r\n"))
					}
				}
			}()
		}
	}()
	return ln.Addr().String()
}

// readCommand reads one command in the array form clients send.
func readCommand(r *bufio.Reader) ([]string, error) {
	header, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	var count int
	if _, err := fmt.Sscanf(strings.TrimSpace(header), "*%d", &count); err != nil {
		return nil, err
	}
	args := make([]string, 0, count)
	for i := 0; i < count; i++ {
		if _, err := r.ReadString('\n'); err != nil { // the length line
			return nil, err
		}
		value, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		args = append(args, strings.TrimRight(value, "\r\n"))
	}
	return args, nil
}

func TestRedisCheck(t *testing.T) {
	c := New(Options{})
	check := func(addr string, o store.DatabaseOptions) store.CheckResult {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return c.Check(ctx, store.Monitor{Type: store.MonitorRedis, Target: addr, Database: o})
	}

	open := fakeRedis(t, "", "", "+PONG")
	if got := check(open, store.DatabaseOptions{}); !got.Up || got.Detail != "PONG" {
		t.Errorf("without a password: %+v", got)
	} else {
		reachedLoopback(t, "without a password", "127.0.0.1", got)
	}

	shared := fakeRedis(t, "", "s3cret", "+PONG")
	if got := check(shared, store.DatabaseOptions{Password: "s3cret"}); !got.Up {
		t.Errorf("shared password: %+v", got)
	}
	// The server's own words go to the log (TestTargetsWordsAreNotStored).
	if got := check(shared, store.DatabaseOptions{}); got.Up || got.Error != "the server answered with an error (NOAUTH)" {
		t.Errorf("no password given: %+v", got)
	}
	if got := check(shared, store.DatabaseOptions{Password: "wrong"}); got.Up || got.Error != "login: the server answered with an error (WRONGPASS)" {
		t.Errorf("wrong password: %+v", got)
	}

	named := fakeRedis(t, "probe", "s3cret", "+PONG")
	if got := check(named, store.DatabaseOptions{Username: "probe", Password: "s3cret"}); !got.Up {
		t.Errorf("user and password: %+v", got)
	}
	if got := check(named, store.DatabaseOptions{Password: "s3cret"}); got.Up {
		t.Errorf("password without its user: %+v", got)
	}

	// Something answers, but it is not Redis saying PONG.
	if got := check(fakeRedis(t, "", "", "+LOADING"), store.DatabaseOptions{}); got.Up || got.Error != "unexpected answer to PING" {
		t.Errorf("unexpected answer: %+v", got)
	}
	// A plain server does not complete a TLS handshake.
	if got := check(open, store.DatabaseOptions{TLS: true, SkipTLSVerify: true}); got.Up {
		t.Errorf("tls against a plain server: %+v", got)
	}
}

// Nothing listens: each database check says so instead of hanging.
func TestDatabaseChecksOfAClosedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	c := New(Options{})
	for _, typ := range []store.MonitorType{store.MonitorPostgres, store.MonitorMySQL, store.MonitorRedis} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		got := c.Check(ctx, store.Monitor{Type: typ, Target: addr,
			Database: store.DatabaseOptions{Username: "kubeat", Password: "s3cret", Database: "shop", Query: "SELECT 1"}})
		cancel()
		if got.Up || got.Error == "" || strings.Contains(got.Error, "s3cret") {
			t.Errorf("%s: %+v", typ, got)
		}
		// Nothing was reached: the one request to the host, nowhere known.
		if len(got.Hops) != 1 || got.Hops[0] != (store.Hop{Host: "127.0.0.1"}) {
			t.Errorf("%s: hops = %v, want one to the host without an address", typ, got.Hops)
		}
	}
}

// Every database check reports the connections it made, at the address
// each reached, whether or not the server then spoke its protocol: here
// one that accepts and hangs up, so that the check fails after connecting.
func TestDatabaseChecksReportTheAddressReached(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	c := New(Options{})
	for _, typ := range []store.MonitorType{store.MonitorPostgres, store.MonitorMySQL, store.MonitorRedis} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		got := c.Check(ctx, store.Monitor{Type: typ, Target: ln.Addr().String(),
			Database: store.DatabaseOptions{Username: "kubeat", Password: "s3cret", Database: "shop", Query: "SELECT 1"}})
		cancel()
		if got.Up {
			t.Errorf("%s: up against a server that hangs up: %+v", typ, got)
		}
		reachedLoopback(t, string(typ), "127.0.0.1", got)
	}
}

// The tests below need real servers and run only when told where they are:
//
//	KUBEAT_TEST_POSTGRES=127.0.0.1:15432 KUBEAT_TEST_MYSQL=127.0.0.1:13306 KUBEAT_TEST_REDIS=127.0.0.1:16379 go test ./internal/checker/
//
// Each expects the user "kubeat" with the password "s3cret" and, for the
// SQL servers, a database "shop"; Redis the password alone.
func realServer(t *testing.T, variable string) string {
	t.Helper()
	addr := os.Getenv(variable)
	if addr == "" {
		t.Skipf("%s is not set", variable)
	}
	return addr
}

func sqlChecks(t *testing.T, typ store.MonitorType, addr string, errors map[string]string) {
	c := New(Options{})
	check := func(edit func(*store.DatabaseOptions)) store.CheckResult {
		o := store.DatabaseOptions{Username: "kubeat", Password: "s3cret", Database: "shop", Query: "SELECT 1"}
		edit(&o)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return c.Check(ctx, store.Monitor{Type: typ, Target: addr, Database: o})
	}

	if got := check(func(*store.DatabaseOptions) {}); !got.Up || got.Detail == "" {
		t.Errorf("SELECT 1: %+v", got)
	}
	if got := check(func(o *store.DatabaseOptions) { o.Query = "SELECT count(*) FROM information_schema.tables" }); !got.Up {
		t.Errorf("a query of one's own: %+v", got)
	}
	for name, edit := range map[string]func(*store.DatabaseOptions){
		"wrong password":   func(o *store.DatabaseOptions) { o.Password = "wrong" },
		"unknown database": func(o *store.DatabaseOptions) { o.Database = "nope" },
		"failing query":    func(o *store.DatabaseOptions) { o.Query = "SELECT * FROM no_such_table" },
	} {
		got := check(edit)
		if got.Up || !strings.Contains(strings.ToLower(got.Error), errors[name]) || strings.Contains(got.Error, "s3cret") {
			t.Errorf("%s: %+v (want an error containing %q)", name, got, errors[name])
		}
	}
}

func TestPostgresCheckAgainstARealServer(t *testing.T) {
	sqlChecks(t, store.MonitorPostgres, realServer(t, "KUBEAT_TEST_POSTGRES"), map[string]string{
		"wrong password":   "password authentication failed",
		"unknown database": `database "nope" does not exist`,
		"failing query":    `relation "no_such_table" does not exist`,
	})
}

func TestMySQLCheckAgainstARealServer(t *testing.T) {
	sqlChecks(t, store.MonitorMySQL, realServer(t, "KUBEAT_TEST_MYSQL"), map[string]string{
		"wrong password":   "access denied",
		"unknown database": "access denied", // MySQL does not tell an unknown database from a forbidden one
		"failing query":    "doesn't exist",
	})
}

func TestRedisCheckAgainstARealServer(t *testing.T) {
	addr := realServer(t, "KUBEAT_TEST_REDIS")
	c := New(Options{})
	check := func(o store.DatabaseOptions) store.CheckResult {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return c.Check(ctx, store.Monitor{Type: store.MonitorRedis, Target: addr, Database: o})
	}
	if got := check(store.DatabaseOptions{Password: "s3cret"}); !got.Up || got.Detail != "PONG" {
		t.Errorf("with the password: %+v", got)
	}
	if got := check(store.DatabaseOptions{}); got.Up || !strings.Contains(got.Error, "NOAUTH") {
		t.Errorf("without the password: %+v", got)
	}
	if got := check(store.DatabaseOptions{Password: "wrong"}); got.Up || !strings.Contains(got.Error, "WRONGPASS") {
		t.Errorf("with a wrong password: %+v", got)
	}
}
