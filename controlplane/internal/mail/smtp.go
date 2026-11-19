// Package mail delivers the control plane's account email (verification,
// password reset, email change) over SMTP. It is the auth.Mailer Gombit asks
// the application for (framework.WithAuthMailer): Gombit owns the account
// lifecycle and the messages (D12), Forge only hands them to a mail server.
package mail

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
	"os"
	"strings"
	"time"

	"github.com/gombit-dev/gombit/auth"
)

// SMTP sends each message through one SMTP transaction with the server at
// Addr. It upgrades to TLS with STARTTLS whenever the server offers it, and
// refuses to send credentials over a connection that is not encrypted, unless
// the server is on loopback (a local relay).
type SMTP struct {
	// Addr is the server's host:port.
	Addr string
	// From is the sender address, e.g. "Forge <no-reply@example.com>".
	From string
	// Username and Password authenticate with PLAIN when Username is set.
	Username string
	Password string
	// Timeout bounds the whole transaction; zero means 30 seconds. The
	// request's context bounds it too.
	Timeout time.Duration
}

var _ auth.Mailer = (*SMTP)(nil)

// Validate reports whether the configuration can send mail.
func (m *SMTP) Validate() error {
	if _, _, err := net.SplitHostPort(m.Addr); err != nil {
		return fmt.Errorf("mail: smtp address %q: %w", m.Addr, err)
	}
	if _, err := mail.ParseAddress(m.From); err != nil {
		return fmt.Errorf("mail: from address %q: %w", m.From, err)
	}
	return nil
}

// Send delivers msg, returning once the server accepted it (auth.Mailer's
// contract: the message's token starts working then) or ctx is done.
func (m *SMTP) Send(ctx context.Context, msg auth.Message) error {
	from, err := mail.ParseAddress(m.From)
	if err != nil {
		return fmt.Errorf("mail: from address: %w", err)
	}
	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("mail: recipient: %w", err)
	}
	body, err := render(from, to, msg, time.Now())
	if err != nil {
		return err
	}

	timeout := m.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", m.Addr)
	if err != nil {
		return fmt.Errorf("mail: dial %s: %w", m.Addr, err)
	}
	// net/smtp has no context: a deadline bounds every read and write, and
	// closing the connection unblocks one in flight when ctx ends first.
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	host, _, _ := net.SplitHostPort(m.Addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("mail: %w", ctxErr(ctx, err))
	}
	defer func() { _ = c.Close() }()
	if err := m.transact(c, host, from.Address, to.Address, body); err != nil {
		return fmt.Errorf("mail: %w", ctxErr(ctx, err))
	}
	return nil
}

func (m *SMTP) transact(c *smtp.Client, host, from, to string, body []byte) error {
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if m.Username != "" {
		// smtp.PlainAuth itself refuses an unencrypted connection to anything
		// but loopback, so a password never crosses the network in the clear.
		if err := c.Auth(smtp.PlainAuth("", m.Username, m.Password, host)); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	// Close is where the server accepts (or refuses) the message.
	if err := w.Close(); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	return c.Quit()
}

// ctxErr adds the context's error when the connection failed because the
// context ended. The connection's deadline is the context's, and either timer
// can fire first, so an I/O timeout is the context's deadline too.
func ctxErr(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return errors.Join(cerr, err)
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return errors.Join(context.DeadlineExceeded, err)
	}
	return err
}

// render builds the RFC 5322 message: Gombit's default subject and plain-text
// body, quoted-printable so any line length and charset survive. The subject is
// encoded, so no header can be injected through it; the addresses are parsed
// ones, rendered by net/mail.
func render(from, to *mail.Address, msg auth.Message, now time.Time) ([]byte, error) {
	if strings.ContainsAny(msg.Subject, "\r\n") {
		return nil, errors.New("mail: subject contains a line break")
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from.String())
	fmt.Fprintf(&b, "To: %s\r\n", to.String())
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", msg.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", now.UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	// Not Binary: the writer emits every line break in Text as CRLF.
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(msg.Text)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
