package alert

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// SMTP connection security modes.
const (
	SMTPStartTLS = "starttls" // upgrade a plain connection; refuse if unsupported
	SMTPTLS      = "tls"      // TLS from the first byte (port 465)
	SMTPNone     = "none"     // no encryption; for a relay on a trusted network
)

// SMTPConfig says how to reach the mail server.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string // empty: no authentication
	Password string
	From     string
	Security string // one of the SMTP* modes
}

// ErrEmailNotConfigured is returned when no SMTP server is configured.
var ErrEmailNotConfigured = errors.New("e-mail is not configured: set UPTIME_SMTP_HOST and UPTIME_SMTP_FROM")

// EmailSender sends notifications as plain-text e-mail.
type EmailSender struct {
	SMTP SMTPConfig
}

// Enabled reports whether an SMTP server is configured.
func (s *EmailSender) Enabled() bool {
	return s.SMTP.Host != "" && s.SMTP.From != ""
}

// ParseRecipients splits a comma-separated address list and validates each
// address. It returns the bare addresses.
func ParseRecipients(list string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		addr, err := mail.ParseAddress(part)
		if err != nil {
			return nil, fmt.Errorf("%q is not a valid e-mail address", part)
		}
		out = append(out, addr.Address)
	}
	if len(out) == 0 {
		return nil, errors.New("at least one e-mail address is required")
	}
	return out, nil
}

func (s *EmailSender) Send(ctx context.Context, ch store.Channel, n Notification) error {
	if !s.Enabled() {
		return ErrEmailNotConfigured
	}
	recipients, err := ParseRecipients(ch.Target)
	if err != nil {
		return err
	}
	return s.deliver(ctx, recipients, buildMessage(s.SMTP.From, recipients, n, time.Now()))
}

// buildMessage renders the notification as an RFC 5322 message.
func buildMessage(from string, to []string, n Notification, now time.Time) []byte {
	var body strings.Builder
	body.WriteString(Text(n))
	body.WriteString("\r\n")
	if facts := Facts(n); len(facts) > 0 {
		body.WriteString("\r\n")
		for _, f := range facts {
			fmt.Fprintf(&body, "%s: %s\r\n", f.Label, f.Value)
		}
	}

	var msg bytes.Buffer
	header := func(name, value string) { fmt.Fprintf(&msg, "%s: %s\r\n", name, value) }
	header("From", from)
	header("To", strings.Join(to, ", "))
	// Q-encoding also removes any line break, so a monitor's name cannot
	// add headers of its own.
	header("Subject", mime.QEncoding.Encode("utf-8", Subject(n)))
	header("Date", now.Format(time.RFC1123Z))
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	header("Auto-Submitted", "auto-generated")
	msg.WriteString("\r\n")

	qp := quotedprintable.NewWriter(&msg)
	_, _ = qp.Write([]byte(body.String()))
	_ = qp.Close()
	return msg.Bytes()
}

func (s *EmailSender) deliver(ctx context.Context, recipients []string, message []byte) error {
	addr := net.JoinHostPort(s.SMTP.Host, fmt.Sprint(s.SMTP.Port))
	tlsConfig := &tls.Config{ServerName: s.SMTP.Host}

	var conn net.Conn
	var err error
	if s.SMTP.Security == SMTPTLS {
		conn, err = (&tls.Dialer{Config: tlsConfig}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	// net/smtp has no context support; bound the whole exchange instead.
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	client, err := smtp.NewClient(conn, s.SMTP.Host)
	if err != nil {
		return err
	}
	defer client.Close()

	if s.SMTP.Security == SMTPStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("the SMTP server does not offer STARTTLS; refusing to send unencrypted")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if s.SMTP.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.SMTP.Username, s.SMTP.Password, s.SMTP.Host)); err != nil {
			return err
		}
	}

	sender, err := mail.ParseAddress(s.SMTP.From)
	if err != nil {
		return fmt.Errorf("sender address: %w", err)
	}
	if err := client.Mail(sender.Address); err != nil {
		return err
	}
	for _, rcpt := range recipients {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(message); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}
