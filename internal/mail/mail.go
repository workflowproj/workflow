// Package mail sends email: through SMTP when WFM_SMTP_HOST is set, otherwise into an
// in-memory mailbox (logged, and readable in development at /api/v1/dev/mailbox). Messages are
// queued as jobs (Queue) so they go out after the change that caused them commits, with retries.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/config"
	"github.com/workflowproj/workflow/internal/jobs"
)

// Message is one email. Text is required; HTML is optional (sent as an alternative).
type Message struct {
	To      []string          `json:"to"`
	Subject string            `json:"subject"`
	Text    string            `json:"text"`
	HTML    string            `json:"html,omitempty"`
	Headers map[string]string `json:"headers,omitempty"` // e.g. List-Unsubscribe
}

// Sender delivers a message now.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// New returns an SMTP sender when a host is configured, else an in-memory mailbox.
func New(cfg config.SMTP, log *slog.Logger) Sender {
	if !cfg.Delivers() {
		return &Memory{Log: log, Max: 200}
	}
	return &SMTP{Cfg: cfg}
}

// Queue enqueues a message for delivery by the worker, inside tx when one is given.
func Queue(ctx context.Context, enq jobs.Enqueuer, tx pgx.Tx, m Message) error {
	if len(m.To) == 0 {
		return nil
	}
	job := jobs.SendEmail{To: m.To, Subject: m.Subject, Text: m.Text, HTML: m.HTML, Headers: m.Headers}
	var err error
	if tx != nil {
		_, err = enq.InsertTx(ctx, tx, job, nil)
	} else {
		_, err = enq.Insert(ctx, job, nil)
	}
	return err
}

// FromJob turns a queued job back into a message.
func FromJob(j jobs.SendEmail) Message {
	return Message{To: j.To, Subject: j.Subject, Text: j.Text, HTML: j.HTML, Headers: j.Headers}
}

// ---------------------------------------------------------------------------
// SMTP

type SMTP struct {
	Cfg config.SMTP
}

func (s *SMTP) Send(ctx context.Context, m Message) error {
	from, err := netmail.ParseAddress(s.Cfg.From)
	if err != nil {
		return fmt.Errorf("WFM_SMTP_FROM: %w", err)
	}
	raw, err := m.Build(from.String(), time.Now())
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(s.Cfg.Host, strconv.Itoa(s.Cfg.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	if s.Cfg.TLS == "tls" {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: s.Cfg.Host, MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(time.Minute))
	c, err := smtp.NewClient(conn, s.Cfg.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if s.Cfg.TLS == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("smtp: server does not offer STARTTLS (set WFM_SMTP_TLS=none to send unencrypted)")
		}
		if err := c.StartTLS(&tls.Config{ServerName: s.Cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if s.Cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Cfg.Username, s.Cfg.Password, s.Cfg.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return err
	}
	for _, to := range m.To {
		if err := c.Rcpt(to); err != nil {
			return fmt.Errorf("smtp rcpt %s: %w", to, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// Build renders the message as MIME (multipart/alternative when there is HTML).
func (m Message) Build(from string, now time.Time) ([]byte, error) {
	if len(m.To) == 0 {
		return nil, errors.New("mail: no recipients")
	}
	var b bytes.Buffer
	h := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	h("From", from)
	h("To", strings.Join(m.To, ", "))
	h("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	h("Date", now.UTC().Format(time.RFC1123Z))
	h("Message-ID", "<"+randomID()+"@wfm>")
	h("MIME-Version", "1.0")
	for k, v := range m.Headers {
		h(textproto.CanonicalMIMEHeaderKey(k), v)
	}
	part := func(w *multipart.Writer, ctype, body string) error {
		pw, err := w.CreatePart(textproto.MIMEHeader{"Content-Type": {ctype + "; charset=utf-8"}, "Content-Transfer-Encoding": {"quoted-printable"}})
		if err != nil {
			return err
		}
		return writeQP(pw, body)
	}
	if m.HTML == "" {
		h("Content-Type", "text/plain; charset=utf-8")
		h("Content-Transfer-Encoding", "quoted-printable")
		b.WriteString("\r\n")
		if err := writeQP(&b, m.Text); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h("Content-Type", "multipart/alternative; boundary="+w.Boundary())
	b.WriteString("\r\n")
	if err := part(w, "text/plain", m.Text); err != nil {
		return nil, err
	}
	if err := part(w, "text/html", m.HTML); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	b.Write(body.Bytes())
	return b.Bytes(), nil
}

func writeQP(w interface{ Write([]byte) (int, error) }, s string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(strings.ReplaceAll(s, "\n", "\r\n"))); err != nil {
		return err
	}
	return qp.Close()
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// In-memory mailbox

// Memory keeps the last Max messages instead of sending them (no SMTP configured).
type Memory struct {
	Log *slog.Logger
	Max int

	mu   sync.Mutex
	msgs []Sent
}

// Sent is a message the in-memory mailbox received.
type Sent struct {
	Message
	At time.Time `json:"at"`
}

func (m *Memory) Send(_ context.Context, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, Sent{Message: msg, At: time.Now()})
	if m.Max > 0 && len(m.msgs) > m.Max {
		m.msgs = m.msgs[len(m.msgs)-m.Max:]
	}
	if m.Log != nil {
		m.Log.Info("email not sent (no WFM_SMTP_HOST)", "to", msg.To, "subject", msg.Subject)
	}
	return nil
}

// Messages returns what the mailbox holds, newest last; with `to`, only messages to that address.
func (m *Memory) Messages(to string) []Sent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Sent{}
	for _, s := range m.msgs {
		if to == "" || containsFold(s.To, to) {
			out = append(out, s)
		}
	}
	return out
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}
