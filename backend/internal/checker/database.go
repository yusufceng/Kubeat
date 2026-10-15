package checker

import (
	"bufio"
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// databaseTLS returns the TLS configuration of a database check, or nil
// when the monitor does not use TLS.
func (c *Checker) databaseTLS(m store.Monitor, host string) *tls.Config {
	if !m.Database.TLS {
		return nil
	}
	return c.tlsConfig(host, m.Database.SkipTLSVerify)
}

// checkPostgres logs in to a PostgreSQL server and runs the monitor's query.
func (c *Checker) checkPostgres(ctx context.Context, m store.Monitor) outcome {
	host, portText, err := net.SplitHostPort(m.Target)
	if err != nil {
		return failed(err)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return failedf("invalid port %q", portText)
	}

	// Parsed from nothing, then every field set here: the settings are the
	// monitor's, not whatever the server's environment has in PG* variables.
	cfg, err := pgx.ParseConfig("")
	if err != nil {
		return failed(err)
	}
	o := m.Database
	cfg.Host, cfg.Port = host, uint16(port)
	cfg.User, cfg.Password, cfg.Database = o.Username, o.Password, o.Database
	cfg.TLSConfig = c.databaseTLS(m, host)
	cfg.Fallbacks = nil
	cfg.RuntimeParams = map[string]string{"application_name": "kubeat"}
	cfg.DialFunc = c.dialer.DialContext
	// The simple protocol runs the query as it is written, without preparing it.
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return failed(postgresError(err))
	}
	// Closing says goodbye to the server; do not let that outlive the check.
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()

	tag, err := conn.Exec(ctx, o.Query)
	if err != nil {
		return failed(postgresError(err))
	}
	return outcome{detail: tag.String()}
}

// postgresError keeps what the server said and drops the connection
// details pgx wraps around it, which repeat the address and the user.
func postgresError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return fmt.Errorf("%s (SQLSTATE %s)", pgErr.Message, pgErr.Code)
	}
	var connErr *pgconn.ConnectError
	if errors.As(err, &connErr) {
		return connErr.Unwrap()
	}
	return err
}

// checkMySQL logs in to a MySQL or MariaDB server and runs the monitor's query.
func (c *Checker) checkMySQL(ctx context.Context, m store.Monitor) outcome {
	host, _, err := net.SplitHostPort(m.Target)
	if err != nil {
		return failed(err)
	}
	o := m.Database
	cfg := mysql.NewConfig()
	cfg.Net, cfg.Addr = "tcp", m.Target
	// The checker's dialer, which the guard watches; the driver's own
	// would connect anywhere.
	cfg.DialFunc = c.dialer.DialContext
	cfg.User, cfg.Passwd, cfg.DBName = o.Username, o.Password, o.Database
	cfg.TLS = c.databaseTLS(m, host)
	// The driver logs broken connections on its own; the check reports them.
	cfg.Logger = discardLogger{}

	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return failed(err)
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	db.SetMaxOpenConns(1)

	rows, err := db.QueryContext(ctx, o.Query)
	if err != nil {
		return failed(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		return failed(err)
	}
	return outcome{detail: fmt.Sprintf("%d rows", count)}
}

type discardLogger struct{}

func (discardLogger) Print(...any) {}

// checkRedis connects to a Redis server, logs in if the monitor has a
// password, and expects PONG for a PING. The protocol is simple enough to
// speak directly: a command is a line, and so is the answer to these.
func (c *Checker) checkRedis(ctx context.Context, m store.Monitor) outcome {
	host, _, err := net.SplitHostPort(m.Target)
	if err != nil {
		return failed(err)
	}
	conn, err := c.dialer.DialContext(ctx, "tcp", m.Target)
	if err != nil {
		return failed(err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if cfg := c.databaseTLS(m, host); cfg != nil {
		secured := tls.Client(conn, cfg)
		if err := secured.HandshakeContext(ctx); err != nil {
			return failedf("tls handshake: %w", err)
		}
		conn = secured
	}
	reader := bufio.NewReader(conn)

	// command sends one command and returns the single line of its answer.
	command := func(args ...string) (string, error) {
		var b strings.Builder
		fmt.Fprintf(&b, "*%d\r\n", len(args))
		for _, arg := range args {
			fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(arg), arg)
		}
		if _, err := conn.Write([]byte(b.String())); err != nil {
			return "", err
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, "-") {
			return "", errors.New(strings.TrimPrefix(line, "-"))
		}
		return line, nil
	}

	o := m.Database
	if o.Password != "" {
		auth := []string{"AUTH", o.Password}
		if o.Username != "" {
			auth = []string{"AUTH", o.Username, o.Password}
		}
		if _, err := command(auth...); err != nil {
			return failedf("login: %w", err)
		}
	}
	answer, err := command("PING")
	if err != nil {
		return failed(err)
	}
	if answer != "+PONG" {
		return failedf("unexpected answer to PING: %s", truncate(answer, 80))
	}
	return outcome{detail: "PONG"}
}
