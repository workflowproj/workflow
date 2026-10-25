package mail

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	netmail "net/mail"
	"strings"
	"testing"
	"time"
)

func TestBuildAlternative(t *testing.T) {
	m := Message{To: []string{"ada@example.com"}, Subject: "PLAT-12 assigned to you — “Checkout”", Text: "Hello\nworld", HTML: "<p>Hello</p>",
		Headers: map[string]string{"list-unsubscribe": "<https://wfm.example.com/u/x>"}}
	raw, err := m.Build("Workflow <wfm@example.com>", time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	msg, err := netmail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if subject != m.Subject {
		t.Fatalf("subject = %q", subject)
	}
	if msg.Header.Get("List-Unsubscribe") == "" {
		t.Fatal("custom headers are kept")
	}
	_, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	r := multipart.NewReader(msg.Body, params["boundary"])
	var bodies []string
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(quotedprintable.NewReader(p))
		bodies = append(bodies, p.Header.Get("Content-Type")+"|"+string(b))
	}
	if len(bodies) != 2 || !strings.Contains(bodies[0], "text/plain") || !strings.Contains(bodies[0], "Hello\r\nworld") || !strings.Contains(bodies[1], "<p>Hello</p>") {
		t.Fatalf("parts = %q", bodies)
	}
}

func TestMemoryMailbox(t *testing.T) {
	m := &Memory{Max: 2}
	for _, to := range []string{"a@x", "b@x", "a@x"} {
		_ = m.Send(context.Background(), Message{To: []string{to}, Subject: to, Text: "t"})
	}
	if got := len(m.Messages("")); got != 2 {
		t.Fatalf("keeps the last Max: %d", got)
	}
	if got := m.Messages("A@X"); len(got) != 1 {
		t.Fatalf("filter by recipient: %d", len(got))
	}
}
