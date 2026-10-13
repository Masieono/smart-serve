package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// smtpSender emails the event. It is the one adapter that is not an HTTP POST, so
// it does not go through doRequest — but it shares the retry rules and the transient
// classification, so a broken mail server behaves like any other broken channel.
type smtpSender struct{}

// NewSMTP returns the SMTP email channel sender.
func NewSMTP() Sender { return smtpSender{} }

func (smtpSender) Type() string { return "smtp" }

func (smtpSender) ValidateConfig(cfg map[string]any) error {
	if stringField(cfg, "host") == "" {
		return errors.New("SMTP host is required")
	}
	if stringField(cfg, "from") == "" {
		return errors.New("from address is required")
	}
	if len(splitList(stringField(cfg, "to"))) == 0 {
		return errors.New("at least one recipient is required")
	}
	_, err := smtpPort(cfg)
	return err
}

// smtpPort resolves the port, treating an empty value as the submission default.
//
// A port that is present but unparseable is an error rather than a silent fallback:
// a typo in that field should not quietly send mail somewhere else.
func smtpPort(cfg map[string]any) (int, error) {
	raw := strings.TrimSpace(stringField(cfg, "port"))
	if raw == "" {
		return 587, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("port %q is not a number", raw)
	}
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("port %d is out of range", port)
	}
	return port, nil
}

func (s smtpSender) Send(ctx context.Context, cfg map[string]any, ev Event) error {
	if err := s.ValidateConfig(cfg); err != nil {
		return err
	}
	message := buildEmail(cfg, ev)
	return retry(ctx, sendAttempts, retryBackoff, func() error {
		return deliverSMTP(ctx, cfg, message)
	})
}

// buildEmail renders the RFC 5322 message. The subject is encoded because it carries
// a device model, and the body is quoted-printable because it carries emoji — a raw
// 8-bit body is not legal and some servers reject it outright.
func buildEmail(cfg map[string]any, ev Event) string {
	to := splitList(stringField(cfg, "to"))

	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", stringField(cfg, "from"))
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", ev.Title()))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	b.WriteString("\r\n")

	w := quotedprintable.NewWriter(&b)
	_, _ = w.Write([]byte(ev.Body()))
	_ = w.Close()
	return b.String()
}

// deliverSMTP performs one delivery attempt.
func deliverSMTP(ctx context.Context, cfg map[string]any, message string) error {
	host := stringField(cfg, "host")
	port, err := smtpPort(cfg)
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	// Port 465 is implicit TLS — the socket is encrypted from the first byte — while
	// 587 and friends start in the clear and upgrade via STARTTLS. Getting that
	// backwards is the usual reason an SMTP config fails with no useful error.
	implicitTLS := port == 465
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}

	dialer := &net.Dialer{Timeout: requestTimeout}
	var conn net.Conn
	if implicitTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return transientError{err}
	}
	// net/smtp has no context support, so the whole conversation is bounded by a
	// deadline instead of by cancellation.
	_ = conn.SetDeadline(time.Now().Add(requestTimeout))

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return transientError{err}
	}
	defer client.Close()

	if !implicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if serr := client.StartTLS(tlsConfig); serr != nil {
				return serr
			}
		}
	}

	if username := stringField(cfg, "username"); username != "" {
		auth := smtp.PlainAuth("", username, stringField(cfg, "password"), host)
		if aerr := client.Auth(auth); aerr != nil {
			return aerr
		}
	}

	if err := client.Mail(stringField(cfg, "from")); err != nil {
		return err
	}
	for _, rcpt := range splitList(stringField(cfg, "to")) {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}

	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(message)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}
