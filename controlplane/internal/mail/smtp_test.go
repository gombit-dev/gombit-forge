package mail

import (
	"bufio"
	"context"
	"errors"
	"io"
	"mime/quotedprintable"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/gombit-dev/gombit/auth"
)

// received is one transaction a fakeServer accepted.
type received struct {
	from, to string
	auth     string
	data     string
}

// fakeServer is a minimal SMTP server on loopback: enough of RFC 5321 for
// net/smtp (EHLO with AUTH, MAIL, RCPT, DATA, QUIT), recording what it got.
func fakeServer(t *testing.T, rejectRcpt bool) (addr string, got <-chan received) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	ch := make(chan received, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		tp := textproto.NewConn(conn)
		var r received
		_ = tp.PrintfLine("220 fake ESMTP")
		for {
			line, err := tp.ReadLine()
			if err != nil {
				return
			}
			verb := strings.ToUpper(strings.Fields(line + " x")[0])
			switch verb {
			case "EHLO", "HELO":
				_ = tp.PrintfLine("250-fake")
				_ = tp.PrintfLine("250 AUTH PLAIN")
			case "AUTH":
				r.auth = line
				_ = tp.PrintfLine("235 ok")
			case "MAIL":
				r.from = line
				_ = tp.PrintfLine("250 ok")
			case "RCPT":
				r.to = line
				if rejectRcpt {
					_ = tp.PrintfLine("550 no such user")
					continue
				}
				_ = tp.PrintfLine("250 ok")
			case "DATA":
				_ = tp.PrintfLine("354 go ahead")
				data, err := io.ReadAll(tp.DotReader())
				if err != nil {
					return
				}
				r.data = string(data)
				_ = tp.PrintfLine("250 queued")
			case "QUIT":
				_ = tp.PrintfLine("221 bye")
				ch <- r
				return
			default:
				_ = tp.PrintfLine("250 ok")
			}
		}
	}()
	return l.Addr().String(), ch
}

func resetMessage() auth.Message {
	return auth.Message{
		Kind:    auth.MessageResetPassword,
		To:      "ada@example.com",
		Subject: "Réinitialiser your password",
		Text:    "Open this link to reset your password:\nhttps://forge.example/reset?token=abc\n",
		Token:   "abc",
	}
}

func TestSMTPSendsTheMessage(t *testing.T) {
	addr, got := fakeServer(t, false)
	m := &SMTP{Addr: addr, From: "Forge <no-reply@forge.example>", Username: "forge", Password: "s3cret"}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := m.Send(context.Background(), resetMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	r := <-got
	if r.from != "MAIL FROM:<no-reply@forge.example>" || !strings.HasPrefix(r.to, "RCPT TO:<ada@example.com>") {
		t.Errorf("envelope = %q / %q", r.from, r.to)
	}
	if !strings.HasPrefix(r.auth, "AUTH PLAIN ") {
		t.Errorf("auth = %q, want PLAIN", r.auth)
	}
	head, body, ok := strings.Cut(r.data, "\n\n")
	if !ok {
		t.Fatalf("no header/body split in %q", r.data)
	}
	for _, want := range []string{
		`From: "Forge" <no-reply@forge.example>`,
		"To: <ada@example.com>",
		"Subject: =?utf-8?q?R=C3=A9initialiser_your_password?=",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: quoted-printable",
	} {
		if !strings.Contains(head, want) {
			t.Errorf("headers lack %q:\n%s", want, head)
		}
	}
	decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decoded), "https://forge.example/reset?token=abc") {
		t.Errorf("body = %q, want the reset link", decoded)
	}
}

// A refused recipient is the message not going out: Send reports it.
func TestSMTPReportsARefusal(t *testing.T) {
	addr, _ := fakeServer(t, true)
	err := (&SMTP{Addr: addr, From: "no-reply@forge.example"}).Send(context.Background(), resetMessage())
	if err == nil || !strings.Contains(err.Error(), "550") {
		t.Fatalf("Send = %v, want the server's 550", err)
	}
}

// Send returns when ctx ends, even against a server that never answers.
func TestSMTPHonorsContext(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	go func() {
		conn, err := l.Accept()
		if err == nil {
			_, _ = bufio.NewReader(conn).ReadString(0) // never greet
			_ = conn.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = (&SMTP{Addr: l.Addr().String(), From: "no-reply@forge.example"}).Send(ctx, resetMessage())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Send = %v, want the context's deadline", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("Send took %s after its context ended", time.Since(start))
	}
}

func TestSMTPRejectsBadInput(t *testing.T) {
	m := &SMTP{Addr: "127.0.0.1:1", From: "no-reply@forge.example"}
	msg := resetMessage()
	msg.Subject = "hi\r\nBcc: everyone@example.com"
	if err := m.Send(context.Background(), msg); err == nil || !strings.Contains(err.Error(), "line break") {
		t.Errorf("Send with a header-injecting subject = %v, want refused", err)
	}
	msg = resetMessage()
	msg.To = "not an address"
	if err := m.Send(context.Background(), msg); err == nil {
		t.Error("Send to an unparseable recipient succeeded")
	}
	for _, bad := range []*SMTP{{Addr: "no-port", From: "a@b.c"}, {Addr: "h:25", From: "nope"}} {
		if err := bad.Validate(); err == nil {
			t.Errorf("Validate(%+v) = nil, want an error", bad)
		}
	}
}
