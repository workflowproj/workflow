package local

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/geoffo-dev/workflow-management/internal/mail"
)

// email is the content of one account email; message renders it as text and HTML.
type email struct {
	To      string
	Subject string
	Heading string
	Lines   []string // paragraphs
	Action  string   // button label
	URL     string
	Footer  string
}

var htmlLayout = template.Must(template.New("email").Parse(`<!doctype html>
<html><body style="margin:0;padding:24px;background:#f6f7f9;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#1f2328">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr><td align="center">
<table role="presentation" width="520" cellpadding="0" cellspacing="0" style="max-width:520px;background:#ffffff;border:1px solid #e3e6ea;border-radius:8px">
<tr><td style="padding:28px 32px">
<p style="margin:0 0 4px;font-size:13px;font-weight:600;color:#59636e">Workflow</p>
<h1 style="margin:0 0 16px;font-size:20px;line-height:1.3">{{.Heading}}</h1>
{{range .Lines}}<p style="margin:0 0 12px;font-size:14px;line-height:1.55">{{.}}</p>{{end}}
{{if .URL}}<p style="margin:20px 0"><a href="{{.URL}}" style="display:inline-block;background:#2f6feb;color:#ffffff;text-decoration:none;font-weight:600;font-size:14px;padding:10px 18px;border-radius:6px">{{.Action}}</a></p>
<p style="margin:0 0 12px;font-size:12px;line-height:1.5;color:#59636e">Or open this link: <a href="{{.URL}}" style="color:#2f6feb;word-break:break-all">{{.URL}}</a></p>{{end}}
{{if .Footer}}<p style="margin:16px 0 0;padding-top:16px;border-top:1px solid #e3e6ea;font-size:12px;line-height:1.5;color:#59636e">{{.Footer}}</p>{{end}}
</td></tr></table>
</td></tr></table>
</body></html>`))

func (e email) message() mail.Message {
	var text strings.Builder
	text.WriteString(e.Heading + "\n\n")
	for _, l := range e.Lines {
		text.WriteString(l + "\n\n")
	}
	if e.URL != "" {
		fmt.Fprintf(&text, "%s:\n%s\n\n", e.Action, e.URL)
	}
	if e.Footer != "" {
		text.WriteString("--\n" + e.Footer + "\n")
	}
	var html bytes.Buffer
	_ = htmlLayout.Execute(&html, e)
	return mail.Message{To: []string{e.To}, Subject: e.Subject, Text: text.String(), HTML: html.String()}
}

const ignoreFooter = "If you did not expect this email, you can ignore it."

func inviteEmail(to, name, inviter, link string, expires time.Time) email {
	return email{
		To: to, Subject: "You're invited to Workflow", Heading: "You're invited to Workflow",
		Lines: []string{
			fmt.Sprintf("Hi %s,", name),
			fmt.Sprintf("%s added you to Workflow. Choose a password to finish setting up your account; you'll sign in with %s.", inviter, to),
			fmt.Sprintf("This link works once and expires on %s.", expires.UTC().Format("2 January 2006 at 15:04 MST")),
		},
		Action: "Accept invitation", URL: link, Footer: ignoreFooter,
	}
}

func resetEmail(to, name, link string) email {
	return email{
		To: to, Subject: "Reset your Workflow password", Heading: "Reset your password",
		Lines: []string{
			fmt.Sprintf("Hi %s,", name),
			"Someone (hopefully you) asked to reset the password of your Workflow account. Choose a new password with the link below; it works once, for one hour.",
		},
		Action: "Choose a new password", URL: link,
		Footer: "If you did not ask for this, ignore this email: your password stays the same.",
	}
}

func securityEmail(to, name, subject, what, ip string, at time.Time, publicURL string) email {
	where := ""
	if ip != "" {
		where = " from " + ip
	}
	return email{
		To: to, Subject: subject, Heading: subject,
		Lines: []string{
			fmt.Sprintf("Hi %s,", name),
			fmt.Sprintf("%s on %s%s.", what, at.UTC().Format("2 January 2006 at 15:04 MST"), where),
			"If this was you, there is nothing to do. If it wasn't, reset your password straight away and tell your administrator.",
		},
		Action: "Reset password", URL: publicURL + "/forgot-password",
		Footer: "You receive this email for every change to how you sign in.",
	}
}
