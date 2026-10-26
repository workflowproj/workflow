package notify

import (
	"bytes"
	_ "embed"
	"fmt"
	htmltemplate "html/template"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Email is the view model of the plain-text and HTML bodies.
type Email struct {
	Brand     string
	Subject   string
	Preheader string // preview text shown by mail clients next to the subject
	Headline  string
	Intro     []string
	Issues    []IssueBlock
	// MoreIssues counts issues left out of a long digest; MoreURL lists everything.
	MoreIssues int
	MoreURL    string
	Button     *Link
	// Footer: why this email arrived and how to change that.
	Reason    string
	ManageURL string
	Unsub     *Link
	TimeZone  string
}

// Link is a labelled URL.
type Link struct {
	Label, URL string
}

// IssueBlock is one issue and what happened to it.
type IssueBlock struct {
	Key, Summary, URL, Project string
	Status, StatusCategory     string
	Events                     []Event
	MoreEvents                 int
	// Open adds a per-issue link (digests; single-issue emails have the button instead).
	Open bool
}

// Event is one notification.
type Event struct {
	Actor, When, What string
	Excerpt           string
}

// ExcerptLines renders a comment excerpt for the HTML template, one line each (joined with
// <br>: mail clients honour white-space: pre-wrap unevenly), with inline Markdown.
func (e Event) ExcerptLines() []htmltemplate.HTML {
	var out []htmltemplate.HTML
	for _, l := range strings.Split(e.Excerpt, "\n") {
		out = append(out, inlineMarkdown(l))
	}
	return out
}

// inlineToken finds the inline Markdown the HTML email renders: `code`, **bold**,
// [text](http(s) URL) and bare http(s) URLs. Everything else stays text.
var inlineToken = regexp.MustCompile("`[^`]+`|\\*\\*[^*]+\\*\\*|\\[[^\\]]+\\]\\(https?://[^\\s()]+\\)|https?://[^\\s<>()\"']+")

// inlineMarkdown renders one line of Markdown safely: every piece of the text is escaped and
// only fixed tags are added, with links limited to http(s).
func inlineMarkdown(line string) htmltemplate.HTML {
	var b strings.Builder
	esc := htmltemplate.HTMLEscapeString
	link := func(href, text string) {
		b.WriteString(`<a href="` + esc(href) + `" class="wfm-link" style="color:#2563eb;">` + esc(text) + `</a>`)
	}
	last := 0
	for _, m := range inlineToken.FindAllStringIndex(line, -1) {
		b.WriteString(esc(line[last:m[0]]))
		tok := line[m[0]:m[1]]
		last = m[1]
		switch {
		case strings.HasPrefix(tok, "`"):
			b.WriteString(`<code style="font-family:SFMono-Regular,Menlo,Consolas,monospace;font-size:13px;">` + esc(tok[1:len(tok)-1]) + `</code>`)
		case strings.HasPrefix(tok, "**"):
			b.WriteString("<strong>" + esc(tok[2:len(tok)-2]) + "</strong>")
		case strings.HasPrefix(tok, "["):
			text, href, _ := strings.Cut(tok[1:len(tok)-1], "](")
			link(href, text)
		default:
			// A bare URL; trailing punctuation belongs to the sentence.
			url := strings.TrimRight(tok, ".,;:!?")
			link(url, url)
			b.WriteString(esc(tok[len(url):]))
		}
	}
	b.WriteString(esc(line[last:]))
	return htmltemplate.HTML(b.String()) // #nosec G203 -- every piece of text is escaped; only fixed tags and http(s) links are added
}

//go:embed templates/email.html
var htmlSource string

var htmlTmpl = htmltemplate.Must(htmltemplate.New("email.html").Funcs(htmltemplate.FuncMap{"pill": pill}).Parse(htmlSource))

// Render produces the plain-text and HTML bodies.
func (e Email) Render() (text, html string, err error) {
	var h bytes.Buffer
	if err := htmlTmpl.Execute(&h, e); err != nil {
		return "", "", err
	}
	return e.text(), h.String(), nil
}

// text is the plain-text body: the same content as the HTML, with URLs written out.
func (e Email) text() string {
	var b strings.Builder
	line := func(parts ...string) {
		b.WriteString(strings.Join(parts, ""))
		b.WriteByte('\n')
	}
	line(e.Headline)
	for _, p := range e.Intro {
		line()
		line(p)
	}
	for _, is := range e.Issues {
		line()
		line(is.Key, ": ", is.Summary)
		status := "Status: " + is.Status
		if is.Project != "" {
			status += " · " + is.Project
		}
		line(status)
		for _, ev := range is.Events {
			line()
			head := ev.Actor
			if ev.When != "" {
				head += " · " + ev.When
			}
			line(head)
			line(ev.What)
			if ev.Excerpt != "" {
				line(quote(ev.Excerpt))
			}
		}
		if is.MoreEvents > 0 {
			line()
			line(fmt.Sprintf("…and %d more", is.MoreEvents))
		}
		if is.Open {
			line()
			line("Open ", is.Key, ": ", is.URL)
		}
	}
	if e.MoreIssues > 0 {
		line()
		line(fmt.Sprintf("And %d more issue%s: %s", e.MoreIssues, plural(e.MoreIssues), e.MoreURL))
	}
	if e.Button != nil {
		line()
		line(e.Button.Label, ": ", e.Button.URL)
	}
	line()
	line("-- ") // signature delimiter: clients fold or strip what follows in replies
	footer := e.Reason
	if e.TimeZone != "" {
		footer += " Times are in " + e.TimeZone + "."
	}
	if footer = strings.TrimSpace(footer); footer != "" {
		line(footer)
	}
	if e.ManageURL != "" {
		line("Manage email notifications: ", e.ManageURL)
	}
	if e.Unsub != nil {
		line(e.Unsub.Label, ": ", e.Unsub.URL)
	}
	return b.String()
}

// pill returns the inline style of a status lozenge (light colours; the template's dark-mode
// rules override them where clients support it).
func pill(category string) htmltemplate.CSS {
	bg, fg := "#eceef2", "#4b5361"
	switch category {
	case "in_progress":
		bg, fg = "#e3ecfe", "#1d4ed8"
	case "done":
		bg, fg = "#e1f4e7", "#15803d"
	}
	return htmltemplate.CSS("background-color:" + bg + ";color:" + fg + ";") // #nosec G203 -- fixed colours, no input
}

// quote marks text as an email quotation.
func quote(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight("> "+l, " ")
	}
	return strings.Join(lines, "\n")
}

// Excerpt shortens comment text for an email, keeping line breaks and cutting on a word.
// Markdown stays as written: plain text shows it as is, HTML renders a safe inline subset.
func Excerpt(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ToValidUTF8(s, ""), "\r\n", "\n"))
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)[:max]
	cut := string(r)
	if i := strings.LastIndexAny(cut, " \n"); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " \n.,;:") + "…"
}
