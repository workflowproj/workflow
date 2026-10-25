package jiraimport

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Rich text conversion options shared by the ADF and wiki converters.
type TextOptions struct {
	// Attachment resolves an attachment file name to a URL here (images and file links).
	Attachment func(filename string) (string, bool)
	// Mention renders a mentioned person; defaults to "@Name".
	Mention func(id, text string) string
}

func (o TextOptions) attachment(name string) (string, bool) {
	if o.Attachment == nil || name == "" {
		return "", false
	}
	return o.Attachment(name)
}

func (o TextOptions) mention(id, text string) string {
	if o.Mention != nil {
		return o.Mention(id, text)
	}
	text = strings.TrimPrefix(text, "@")
	if text == "" {
		text = id
	}
	return "@" + text
}

// adfNode is a node of the Atlassian Document Format.
type adfNode struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Attrs   map[string]any `json:"attrs"`
	Marks   []adfMark      `json:"marks"`
	Content []adfNode      `json:"content"`
}

type adfMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs"`
}

func (n adfNode) attr(k string) string {
	switch v := n.Attrs[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

// RichText converts a Jira rich-text value to Markdown: an ADF document (Cloud, REST v3) or
// a wiki-markup string (Data Center, REST v2).
func RichText(raw json.RawMessage, opts TextOptions) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return WikiToMarkdown(s, opts)
	}
	return ADFToMarkdown(raw, opts)
}

// ADFToMarkdown converts an Atlassian Document Format document to GitHub-flavoured Markdown.
// Unknown nodes keep their text content so nothing typed by a person is lost.
func ADFToMarkdown(raw json.RawMessage, opts TextOptions) string {
	var doc adfNode
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	c := &adfConv{opts: opts}
	out := c.blocks(doc.Content, "")
	return strings.TrimSpace(collapseBlankLines(out))
}

type adfConv struct {
	opts TextOptions
}

// blocks renders block nodes separated by blank lines; prefix is prepended to every line
// (quotes, list continuation indentation).
func (c *adfConv) blocks(nodes []adfNode, prefix string) string {
	var parts []string
	for _, n := range nodes {
		if s := c.block(n, prefix); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n"+strings.TrimRight(prefix, " ")+"\n")
}

func prefixLines(s, first, rest string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		p := rest
		if i == 0 {
			p = first
		}
		if l == "" {
			lines[i] = strings.TrimRight(p, " ")
		} else {
			lines[i] = p + l
		}
	}
	return strings.Join(lines, "\n")
}

func (c *adfConv) block(n adfNode, prefix string) string {
	switch n.Type {
	case "paragraph":
		return prefixLines(c.inline(n.Content), prefix, prefix)
	case "heading":
		level, _ := strconv.Atoi(n.attr("level"))
		level = min(max(level, 1), 6)
		return prefix + strings.Repeat("#", level) + " " + strings.ReplaceAll(c.inline(n.Content), "\n", " ")
	case "bulletList", "orderedList", "taskList", "decisionList":
		return c.list(n, prefix)
	case "codeBlock":
		var b strings.Builder
		for _, t := range n.Content {
			b.WriteString(t.Text)
		}
		fence := "```"
		for strings.Contains(b.String(), fence) {
			fence += "`"
		}
		return prefixLines(fence+n.attr("language")+"\n"+strings.TrimRight(b.String(), "\n")+"\n"+fence, prefix, prefix)
	case "blockquote":
		return c.blocks(n.Content, prefix+"> ")
	case "panel":
		label := map[string]string{"info": "Info", "note": "Note", "warning": "Warning", "error": "Error", "success": "Success", "tip": "Tip"}[n.attr("panelType")]
		body := c.blocks(n.Content, prefix+"> ")
		if label != "" {
			body = prefix + "> **" + label + ":**\n" + strings.TrimRight(prefix+">", " ") + "\n" + body
		}
		return body
	case "rule":
		return prefix + "---"
	case "mediaSingle", "mediaGroup":
		var parts []string
		for _, m := range n.Content {
			if s := c.media(m); s != "" {
				parts = append(parts, s)
			}
		}
		return prefixLines(strings.Join(parts, "\n"), prefix, prefix)
	case "media":
		return prefix + c.media(n)
	case "table":
		return prefixLines(c.table(n), prefix, prefix)
	case "expand", "nestedExpand":
		title := n.attr("title")
		body := c.blocks(n.Content, prefix)
		if title != "" {
			return prefix + "**" + title + "**\n" + strings.TrimRight(prefix, " ") + "\n" + body
		}
		return body
	case "blockCard", "embedCard":
		u := n.attr("url")
		if u == "" {
			return ""
		}
		return prefix + "<" + u + ">"
	case "extension", "bodiedExtension":
		if len(n.Content) > 0 {
			return c.blocks(n.Content, prefix)
		}
		return ""
	}
	// Inline content at block level, or an unknown block: keep the text.
	if n.Text != "" || isInline(n.Type) {
		return prefixLines(c.inline([]adfNode{n}), prefix, prefix)
	}
	return c.blocks(n.Content, prefix)
}

func isInline(t string) bool {
	switch t {
	case "text", "hardBreak", "mention", "emoji", "inlineCard", "status", "date", "placeholder":
		return true
	}
	return false
}

func (c *adfConv) list(n adfNode, prefix string) string {
	start := 1
	if s, err := strconv.Atoi(n.attr("order")); err == nil && s > 0 {
		start = s
	}
	var items []string
	for k, it := range n.Content {
		var marker string
		switch n.Type {
		case "orderedList":
			marker = strconv.Itoa(start+k) + ". "
		case "taskList":
			if it.attr("state") == "DONE" {
				marker = "- [x] "
			} else {
				marker = "- [ ] "
			}
		default:
			marker = "- "
		}
		indent := strings.Repeat(" ", len(marker))
		var body string
		if it.Type == "taskItem" || it.Type == "decisionItem" {
			// Task and decision items hold inline content directly.
			body = c.inline(it.Content)
			body = prefixLines(body, "", indent)
		} else {
			// listItem: paragraphs and nested lists; nested blocks are indented under the marker.
			var parts []string
			for j, child := range it.Content {
				s := c.block(child, "")
				if s == "" {
					continue
				}
				if j > 0 && child.Type != "bulletList" && child.Type != "orderedList" && child.Type != "taskList" {
					s = "\n" + s
				}
				parts = append(parts, s)
			}
			body = prefixLines(strings.Join(parts, "\n"), "", indent)
		}
		items = append(items, prefixLines(marker+body, prefix, prefix))
	}
	return strings.Join(items, "\n")
}

func (c *adfConv) media(n adfNode) string {
	name := cmpOr(n.attr("alt"), n.attr("__fileName"), n.attr("fileName"))
	if n.attr("type") == "link" || n.attr("type") == "external" {
		if u := n.attr("url"); u != "" {
			return "![" + escapeText(name) + "](" + u + ")"
		}
	}
	if u, ok := c.opts.attachment(name); ok {
		return "![" + escapeText(name) + "](" + u + ")"
	}
	if name != "" {
		return "*[attachment: " + escapeText(name) + "]*"
	}
	return "*[attachment]*"
}

func (c *adfConv) table(n adfNode) string {
	var rows [][]string
	header := false
	for r, row := range n.Content {
		var cells []string
		for _, cell := range row.Content {
			if r == 0 && cell.Type == "tableHeader" {
				header = true
			}
			text := c.blocks(cell.Content, "")
			text = strings.ReplaceAll(strings.TrimSpace(text), "\n\n", "<br>")
			text = strings.ReplaceAll(text, "\n", "<br>")
			cells = append(cells, strings.ReplaceAll(text, "|", "\\|"))
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		return ""
	}
	width := 0
	for _, r := range rows {
		width = max(width, len(r))
	}
	if !header {
		// Markdown tables need a header row; use an empty one.
		rows = append([][]string{make([]string, width)}, rows...)
	}
	var b strings.Builder
	for i, r := range rows {
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

// inline renders inline nodes, applying marks to text runs.
func (c *adfConv) inline(nodes []adfNode) string {
	var b strings.Builder
	for _, n := range nodes {
		switch n.Type {
		case "text":
			b.WriteString(applyMarks(n.Text, n.Marks))
		case "hardBreak":
			b.WriteString("  \n")
		case "mention":
			b.WriteString(c.opts.mention(n.attr("id"), n.attr("text")))
		case "emoji":
			b.WriteString(cmpOr(n.attr("text"), n.attr("shortName")))
		case "inlineCard":
			if u := n.attr("url"); u != "" {
				b.WriteString("<" + u + ">")
			}
		case "status":
			b.WriteString("`" + strings.ToUpper(n.attr("text")) + "`")
		case "date":
			if ms, err := strconv.ParseInt(n.attr("timestamp"), 10, 64); err == nil {
				b.WriteString(time.UnixMilli(ms).UTC().Format("2006-01-02"))
			}
		case "placeholder":
		case "media", "mediaInline":
			b.WriteString(c.media(n))
		default:
			b.WriteString(c.inline(n.Content))
		}
	}
	return b.String()
}

// applyMarks wraps a text run in Markdown emphasis, code and links.
func applyMarks(text string, marks []adfMark) string {
	code := false
	for _, m := range marks {
		if m.Type == "code" {
			code = true
		}
	}
	if code {
		fence := "`"
		for strings.Contains(text, fence) {
			fence += "`"
		}
		pad := ""
		if strings.HasPrefix(text, "`") || strings.HasSuffix(text, "`") {
			pad = " "
		}
		text = fence + pad + text + pad + fence
	} else {
		text = escapeText(text)
	}
	// Emphasis markers must hug non-space characters: move surrounding spaces outside.
	lead := text[:len(text)-len(strings.TrimLeft(text, " "))]
	trail := text[len(strings.TrimRight(text, " ")):]
	core := strings.TrimSpace(text)
	if core == "" {
		return text
	}
	for _, m := range marks {
		switch m.Type {
		case "strong":
			core = "**" + core + "**"
		case "em":
			core = "*" + core + "*"
		case "strike":
			core = "~~" + core + "~~"
		case "link":
			if href, _ := m.Attrs["href"].(string); href != "" {
				core = "[" + core + "](" + strings.ReplaceAll(href, ")", "%29") + ")"
			}
		}
	}
	return lead + core + trail
}

// escapeText escapes characters that would otherwise start Markdown syntax mid-text.
func escapeText(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "`", "\\`", "[", `\[`, "]", `\]`, "<", `\<`)
	s = r.Replace(s)
	// A leading "#", ">" or "1." would turn a paragraph into a heading, quote or list.
	trimmed := strings.TrimLeft(s, " ")
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "+ ") {
		s = s[:len(s)-len(trimmed)] + `\` + trimmed
	}
	return s
}

func collapseBlankLines(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}
