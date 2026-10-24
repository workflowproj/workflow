package jiraimport

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// WikiToMarkdown converts Jira wiki markup (Data Center / Server) to GitHub-flavoured Markdown:
// headings, emphasis, monospace, code and noformat blocks, quotes, panels, lists, tables,
// links, mentions, images and attachments. Macros it does not know keep their text.
func WikiToMarkdown(src string, opts TextOptions) string {
	src = strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\r", "\n")
	w := &wikiConv{opts: opts}
	return strings.TrimSpace(collapseBlankLines(w.blocks(strings.Split(src, "\n"))))
}

type wikiConv struct {
	opts TextOptions
}

var (
	wikiHeading  = regexp.MustCompile(`^\s*h([1-6])\.\s*(.*)$`)
	wikiQuote    = regexp.MustCompile(`^\s*bq\.\s*(.*)$`)
	wikiList     = regexp.MustCompile(`^\s*([*#]+|-)\s+(.*)$`)
	wikiRule     = regexp.MustCompile(`^\s*-{4,}\s*$`)
	wikiBlockTag = regexp.MustCompile(`^\s*\{(code|noformat|quote|panel)((?::[^}]*)?)\}`)
	wikiColor    = regexp.MustCompile(`\{color(?::[^}]*)?\}`)
	wikiAnchor   = regexp.MustCompile(`\{anchor(?::[^}]*)?\}`)
)

func (w *wikiConv) blocks(lines []string) string {
	var out []string
	var para []string
	flush := func() {
		if len(para) > 0 {
			out = append(out, strings.Join(para, "  \n"))
			para = nil
		}
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if m := wikiBlockTag.FindStringSubmatch(line); m != nil {
			flush()
			tag, params := m[1], strings.TrimPrefix(m[2], ":")
			// Collect the body up to the closing tag, which may be on the same line.
			rest := line[strings.Index(line, m[0])+len(m[0]):]
			closing := "{" + tag + "}"
			var body []string
			after := ""
			if k := strings.Index(rest, closing); k >= 0 {
				body = append(body, rest[:k])
				after = rest[k+len(closing):]
			} else {
				if strings.TrimSpace(rest) != "" {
					body = append(body, rest)
				}
				j := i + 1
				for ; j < len(lines); j++ {
					if k := strings.Index(lines[j], closing); k >= 0 {
						body = append(body, lines[j][:k])
						after = lines[j][k+len(closing):]
						break
					}
					body = append(body, lines[j])
				}
				i = j
			}
			out = append(out, w.blockMacro(tag, params, body))
			if strings.TrimSpace(after) != "" {
				para = append(para, w.inline(after))
			}
			continue
		}
		switch {
		case trimmed == "":
			flush()
		case wikiHeading.MatchString(line):
			flush()
			m := wikiHeading.FindStringSubmatch(line)
			out = append(out, strings.Repeat("#", int(m[1][0]-'0'))+" "+w.inline(m[2]))
		case wikiQuote.MatchString(line):
			flush()
			out = append(out, "> "+w.inline(wikiQuote.FindStringSubmatch(line)[1]))
		case wikiRule.MatchString(line):
			flush()
			out = append(out, "---")
		case wikiList.MatchString(line) && !strings.HasPrefix(trimmed, "---"):
			flush()
			j := i
			var items []string
			for j < len(lines) && wikiList.MatchString(lines[j]) && !wikiRule.MatchString(lines[j]) {
				items = append(items, lines[j])
				j++
			}
			out = append(out, w.list(items))
			i = j - 1
		case strings.HasPrefix(trimmed, "|"):
			flush()
			j := i
			var rows []string
			for j < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[j]), "|") {
				rows = append(rows, strings.TrimSpace(lines[j]))
				j++
			}
			out = append(out, w.table(rows))
			i = j - 1
		default:
			para = append(para, w.inline(line))
		}
	}
	flush()
	return strings.Join(out, "\n\n")
}

func (w *wikiConv) blockMacro(tag, params string, body []string) string {
	switch tag {
	case "code", "noformat":
		lang := ""
		if tag == "code" {
			for _, p := range strings.Split(params, "|") {
				p = strings.TrimSpace(p)
				if k, v, ok := strings.Cut(p, "="); ok {
					if k == "language" || k == "lang" {
						lang = v
					}
				} else if p != "" {
					lang = p
				}
			}
		}
		text := strings.Trim(strings.Join(body, "\n"), "\n")
		fence := "```"
		for strings.Contains(text, fence) {
			fence += "`"
		}
		return fence + strings.ToLower(lang) + "\n" + text + "\n" + fence
	default: // quote, panel
		inner := w.blocks(body)
		title := ""
		if tag == "panel" {
			for _, p := range strings.Split(params, "|") {
				if k, v, ok := strings.Cut(strings.TrimSpace(p), "="); ok && k == "title" {
					title = v
				}
			}
		}
		if title != "" {
			inner = "**" + title + "**\n\n" + inner
		}
		return prefixLines(inner, "> ", "> ")
	}
}

func (w *wikiConv) list(items []string) string {
	var out []string
	top := byte(0)
	for _, line := range items {
		m := wikiList.FindStringSubmatch(line)
		markers := m[1]
		if markers == "-" {
			markers = "*"
		}
		// A bullet list followed by a numbered one (or the reverse) are two lists.
		if len(markers) == 1 {
			if top != 0 && top != markers[0] {
				out = append(out, "")
			}
			top = markers[0]
		}
		indent := ""
		for _, c := range markers[:len(markers)-1] {
			if c == '#' {
				indent += "   "
			} else {
				indent += "  "
			}
		}
		marker := "- "
		if markers[len(markers)-1] == '#' {
			marker = "1. "
		}
		out = append(out, indent+marker+w.inline(m[2]))
	}
	return strings.Join(out, "\n")
}

func (w *wikiConv) table(rows []string) string {
	var cells [][]string
	header := false
	width := 0
	for r, row := range rows {
		isHeader := strings.HasPrefix(row, "||")
		if r == 0 && isHeader {
			header = true
		}
		var cs []string
		for _, c := range splitCells(row) {
			cs = append(cs, strings.ReplaceAll(w.inline(strings.TrimSpace(c)), "|", "\\|"))
		}
		width = max(width, len(cs))
		cells = append(cells, cs)
	}
	if !header {
		cells = append([][]string{make([]string, width)}, cells...)
	}
	var b strings.Builder
	for i, r := range cells {
		for len(r) < width {
			r = append(r, "")
		}
		b.WriteString("| " + strings.Join(r, " | ") + " |\n")
		if i == 0 {
			b.WriteString("|" + strings.Repeat(" --- |", width) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// splitCells splits a table row on | and ||, leaving pipes inside [links] and {{code}} alone.
func splitCells(row string) []string {
	var cells []string
	var cur strings.Builder
	depth, mono := 0, false
	for i := 0; i < len(row); i++ {
		c := row[i]
		switch {
		case strings.HasPrefix(row[i:], "{{"):
			mono = true
			cur.WriteString("{{")
			i++
			continue
		case strings.HasPrefix(row[i:], "}}"):
			mono = false
			cur.WriteString("}}")
			i++
			continue
		case c == '[' && !mono:
			depth++
		case c == ']' && !mono && depth > 0:
			depth--
		case c == '|' && depth == 0 && !mono:
			if i+1 < len(row) && row[i+1] == '|' {
				i++
			}
			cells = append(cells, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	if strings.TrimSpace(cur.String()) != "" {
		cells = append(cells, cur.String())
	}
	if len(cells) > 0 && strings.TrimSpace(cells[0]) == "" {
		cells = cells[1:]
	}
	return cells
}

// inline converts the inline markup of one line.
func (w *wikiConv) inline(s string) string {
	s = wikiColor.ReplaceAllString(s, "")
	s = wikiAnchor.ReplaceAllString(s, "")
	var b strings.Builder
	for i := 0; i < len(s); {
		rest := s[i:]
		switch {
		case strings.HasPrefix(rest, `\\`):
			b.WriteString("  \n")
			i += 2
			continue
		case rest[0] == '\\' && len(rest) > 1:
			// Escaped markup character.
			r, n := utf8.DecodeRuneInString(rest[1:])
			b.WriteString(mdEscapeRune(r))
			i += 1 + n
			continue
		case strings.HasPrefix(rest, "{{"):
			if end := strings.Index(rest[2:], "}}"); end >= 0 {
				b.WriteString(codeSpan(rest[2 : 2+end]))
				i += end + 4
				continue
			}
		case rest[0] == '[':
			if end := strings.IndexByte(rest, ']'); end > 0 {
				if md, ok := w.link(rest[1:end]); ok {
					b.WriteString(md)
					i += end + 1
					continue
				}
			}
		case rest[0] == '!':
			if end := strings.IndexByte(rest[1:], '!'); end > 0 {
				if md, ok := w.image(rest[1 : 1+end]); ok {
					b.WriteString(md)
					i += end + 2
					continue
				}
			}
		}
		if md, n, ok := w.emphasis(s, i); ok {
			b.WriteString(md)
			i += n
			continue
		}
		r, n := utf8.DecodeRuneInString(rest)
		switch r {
		case '`', '*', '_', '~':
			b.WriteString(`\` + string(r))
		case '<':
			b.WriteString(`\<`)
		default:
			b.WriteRune(r)
		}
		i += n
	}
	return b.String()
}

func mdEscapeRune(r rune) string {
	if strings.ContainsRune("\\`*_{}[]()#+-.!|<>~^", r) {
		return `\` + string(r)
	}
	return string(r)
}

func codeSpan(text string) string {
	fence := "`"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	pad := ""
	if strings.HasPrefix(text, "`") || strings.HasSuffix(text, "`") {
		pad = " "
	}
	return fence + pad + text + pad + fence
}

var emphasis = map[string][2]string{
	"*": {"**", "**"}, "_": {"*", "*"}, "??": {"*", "*"}, "-": {"~~", "~~"},
	"+": {"", ""}, "^": {"", ""}, "~": {"", ""},
}

// emphasis recognises *strong*, _emphasis_, ??citation??, -deleted-, +inserted+, ^sup^ and ~sub~
// at s[i]: the opening marker follows a non-word character and precedes a non-space; the
// closing one follows a non-space and precedes a non-word character.
func (w *wikiConv) emphasis(s string, i int) (string, int, bool) {
	marker := ""
	for _, m := range []string{"??", "*", "_", "-", "+", "^", "~"} {
		if strings.HasPrefix(s[i:], m) {
			marker = m
			break
		}
	}
	if marker == "" {
		return "", 0, false
	}
	if i > 0 {
		prev, _ := utf8.DecodeLastRuneInString(s[:i])
		if isWordRune(prev) || strings.ContainsRune(marker, prev) {
			return "", 0, false
		}
	}
	start := i + len(marker)
	if start >= len(s) {
		return "", 0, false
	}
	first, _ := utf8.DecodeRuneInString(s[start:])
	if unicode.IsSpace(first) || strings.HasPrefix(s[start:], marker) {
		return "", 0, false
	}
	for j := start + 1; j <= len(s)-len(marker); j++ {
		if !strings.HasPrefix(s[j:], marker) {
			continue
		}
		last, _ := utf8.DecodeLastRuneInString(s[:j])
		if unicode.IsSpace(last) {
			continue
		}
		after := j + len(marker)
		if after < len(s) {
			next, _ := utf8.DecodeRuneInString(s[after:])
			if isWordRune(next) {
				continue
			}
		}
		inner := w.inline(s[start:j])
		wrap := emphasis[marker]
		return wrap[0] + inner + wrap[1], after - i, true
	}
	return "", 0, false
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

var urlLike = regexp.MustCompile(`^(?i)(https?://|mailto:|ftp://|file:)`)

// link converts [text|target] forms: URLs, mentions ([~user]), attachments ([^file]) and
// anchors. Not a link: the brackets stay.
func (w *wikiConv) link(inner string) (string, bool) {
	text, target, hasText := strings.Cut(inner, "|")
	if !hasText {
		target, text = inner, ""
	} else if k := strings.IndexByte(target, '|'); k >= 0 {
		target = target[:k] // [text|url|tooltip]
	}
	target = strings.TrimSpace(target)
	switch {
	case strings.HasPrefix(target, "~"):
		id := strings.TrimPrefix(strings.TrimPrefix(target, "~"), "accountid:")
		return w.opts.mention(id, text), true
	case strings.HasPrefix(target, "^"):
		name := target[1:]
		label := cmpOr(text, name)
		if u, ok := w.opts.attachment(name); ok {
			return "[" + escapeText(label) + "](" + u + ")", true
		}
		return "*[attachment: " + escapeText(name) + "]*", true
	case strings.HasPrefix(target, "#"):
		return escapeText(cmpOr(text, target[1:])), true
	case urlLike.MatchString(target):
		if text == "" {
			return "<" + target + ">", true
		}
		return "[" + w.inline(text) + "](" + strings.ReplaceAll(target, ")", "%29") + ")", true
	case issueKeyLike.MatchString(target):
		return cmpOr(text, target), true
	}
	return "", false
}

var issueKeyLike = regexp.MustCompile(`^[A-Z][A-Z0-9_]+-\d+$`)

// image converts !file.png!, !file.png|thumbnail! and !http://host/img.png!.
func (w *wikiConv) image(inner string) (string, bool) {
	name, _, _ := strings.Cut(inner, "|")
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, " \t\n") && !urlLike.MatchString(name) {
		return "", false
	}
	if urlLike.MatchString(name) {
		return "![](" + name + ")", true
	}
	if !strings.Contains(name, ".") {
		return "", false
	}
	if u, ok := w.opts.attachment(name); ok {
		return "![" + escapeText(name) + "](" + u + ")", true
	}
	return "*[attachment: " + escapeText(name) + "]*", true
}
