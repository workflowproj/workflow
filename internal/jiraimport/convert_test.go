package jiraimport

import (
	"encoding/json"
	"strings"
	"testing"
)

func attachments(names ...string) TextOptions {
	return TextOptions{Attachment: func(name string) (string, bool) {
		for _, n := range names {
			if n == name {
				return "/api/v1/attachments/" + name, true
			}
		}
		return "", false
	}}
}

func TestADFToMarkdown(t *testing.T) {
	cases := []struct {
		name string
		adf  string
		want string
	}{
		{
			name: "paragraph with marks",
			adf: `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[
				{"type":"text","text":"Hello "},
				{"type":"text","text":"bold","marks":[{"type":"strong"}]},
				{"type":"text","text":", "},
				{"type":"text","text":"italic ","marks":[{"type":"em"}]},
				{"type":"text","text":"code","marks":[{"type":"code"}]},
				{"type":"text","text":" and "},
				{"type":"text","text":"a link","marks":[{"type":"link","attrs":{"href":"https://example.com/a"}}]},
				{"type":"text","text":" "},
				{"type":"text","text":"gone","marks":[{"type":"strike"}]}]}]}`,
			want: "Hello **bold**, *italic* `code` and [a link](https://example.com/a) ~~gone~~",
		},
		{
			name: "headings, rule, quote and hard break",
			adf: `{"type":"doc","content":[
				{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Steps"}]},
				{"type":"rule"},
				{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"line one"},{"type":"hardBreak"},{"type":"text","text":"line two"}]}]}]}`,
			want: "## Steps\n\n---\n\n> line one  \n> line two",
		},
		{
			name: "nested lists",
			adf: `{"type":"doc","content":[{"type":"bulletList","content":[
				{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]},
					{"type":"orderedList","attrs":{"order":3},"content":[
						{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"three"}]}]},
						{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"four"}]}]}]}]},
				{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"two"}]}]}]}]}`,
			want: "- one\n  3. three\n  4. four\n- two",
		},
		{
			name: "code block with language",
			adf:  `{"type":"doc","content":[{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"func main() {\n\tprintln(\"*hi*\")\n}"}]}]}`,
			want: "```go\nfunc main() {\n\tprintln(\"*hi*\")\n}\n```",
		},
		{
			name: "mention, emoji, status, date, inline card",
			adf: `{"type":"doc","content":[{"type":"paragraph","content":[
				{"type":"mention","attrs":{"id":"abc","text":"@Ada Lovelace"}},{"type":"text","text":" "},
				{"type":"emoji","attrs":{"shortName":":smile:","text":"😄"}},{"type":"text","text":" "},
				{"type":"status","attrs":{"text":"in review","color":"blue"}},{"type":"text","text":" "},
				{"type":"date","attrs":{"timestamp":"1704067200000"}},{"type":"text","text":" "},
				{"type":"inlineCard","attrs":{"url":"https://example.com/x"}}]}]}`,
			want: "@Ada Lovelace 😄 `IN REVIEW` 2024-01-01 <https://example.com/x>",
		},
		{
			name: "table with header",
			adf: `{"type":"doc","content":[{"type":"table","content":[
				{"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Key"}]}]},{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Value"}]}]}]},
				{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"a|b"}]}]},{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"1"}]}]}]}]}]}`,
			want: "| Key | Value |\n| --- | --- |\n| a\\|b | 1 |",
		},
		{
			name: "table cells with several lines stay on one line, without HTML",
			adf: `{"type":"doc","content":[{"type":"table","content":[
				{"type":"tableRow","content":[{"type":"tableCell","content":[
					{"type":"paragraph","content":[{"type":"text","text":"first"},{"type":"hardBreak"},{"type":"text","text":"second"}]},
					{"type":"paragraph","content":[{"type":"text","text":"third"}]}]}]}]}]}`,
			want: "|  |\n| --- |\n| first · second · third |",
		},
		{
			name: "task list, panel and media",
			adf: `{"type":"doc","content":[
				{"type":"taskList","content":[{"type":"taskItem","attrs":{"state":"DONE"},"content":[{"type":"text","text":"done"}]},{"type":"taskItem","attrs":{"state":"TODO"},"content":[{"type":"text","text":"todo"}]}]},
				{"type":"panel","attrs":{"panelType":"warning"},"content":[{"type":"paragraph","content":[{"type":"text","text":"careful"}]}]},
				{"type":"mediaSingle","content":[{"type":"media","attrs":{"type":"file","id":"m1","alt":"shot.png"}}]},
				{"type":"mediaGroup","content":[{"type":"media","attrs":{"type":"file","id":"m2","alt":"spec.pdf"}}]}]}`,
			want: "- [x] done\n- [ ] todo\n\n> **Warning:**\n>\n> careful\n\n![shot.png](/api/v1/attachments/shot.png)\n\n*[attachment: spec.pdf]*",
		},
		{
			name: "markdown characters in text are escaped",
			adf:  `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"# not a heading with *stars* and [brackets]"}]}]}`,
			want: `\# not a heading with \*stars\* and \[brackets\]`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ADFToMarkdown(json.RawMessage(c.adf), attachments("shot.png"))
			if got != c.want {
				t.Fatalf("got:\n%s\n\nwant:\n%s", got, c.want)
			}
		})
	}
}

func TestRichTextPicksFormat(t *testing.T) {
	if got := RichText(json.RawMessage(`"*bold* text"`), TextOptions{}); got != "**bold** text" {
		t.Fatalf("wiki string: %q", got)
	}
	if got := RichText(json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"adf"}]}]}`), TextOptions{}); got != "adf" {
		t.Fatalf("adf: %q", got)
	}
	if got := RichText(json.RawMessage(`null`), TextOptions{}); got != "" {
		t.Fatalf("null: %q", got)
	}
}

func TestWikiToMarkdown(t *testing.T) {
	cases := []struct {
		name, wiki, want string
	}{
		{"headings", "h1. Title\nh3. Sub *bold*", "# Title\n\n### Sub **bold**"},
		{"emphasis", "*strong* _emph_ -gone- +ins+ ??cite?? {{mono}} x^2^ H~2~O and ^sup^", "**strong** *emph* ~~gone~~ ins *cite* `mono` x^2^ H\\~2\\~O and sup"},
		{"no emphasis inside words", "snake_case_name and a-b-c and 2*3*4", "snake\\_case\\_name and a-b-c and 2\\*3\\*4"},
		{"links", "[Example|https://example.com] and [https://x.io] and [~jsmith] and [~accountid:5b10a2]", "[Example](https://example.com) and <https://x.io> and @jsmith and @5b10a2"},
		{"lists", "* one\n** one.a\n* two\n# first\n# second\n#* mixed", "- one\n  - one.a\n- two\n\n1. first\n1. second\n   - mixed"},
		{"dash list", "- alpha\n- beta", "- alpha\n- beta"},
		{"code block", "{code:java}\nint x = 1; // *not bold*\n{code}", "```java\nint x = 1; // *not bold*\n```"},
		{"code block with params", "{code:title=A.java|language=java|borderStyle=solid}a{code}", "```java\na\n```"},
		{"noformat", "{noformat}\n_raw_ text\n{noformat}", "```\n_raw_ text\n```"},
		{"quote macro", "{quote}\nquoted *text*\n{quote}", "> quoted **text**"},
		{"bq", "bq. a quote", "> a quote"},
		{"panel", "{panel:title=Note|borderStyle=dashed}\nbody\n{panel}", "> **Note**\n>\n> body"},
		{"table", "||Key||Value||\n|a|[link|http://x.y]|\n|b|{{c|d}}|", "| Key | Value |\n| --- | --- |\n| a | [link](http://x.y) |\n| b | `c\\|d` |"},
		{"rule and breaks", "one\\\\two\n----\nthree\nfour", "one  \ntwo\n\n---\n\nthree  \nfour"},
		{"images and attachments", "!shot.png|thumbnail! !missing.png! [^spec.pdf] !http://img.example/a.png!", "![shot.png](/api/v1/attachments/shot.png) *[attachment: missing.png]* *[attachment: spec.pdf]* ![](http://img.example/a.png)"},
		{"color macro", "{color:red}red text{color}", "red text"},
		{"escaped markup", `\*literal\* stars`, `\*literal\* stars`},
		{"issue key link", "see [PLAT-12]", "see PLAT-12"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := WikiToMarkdown(c.wiki, attachments("shot.png"))
			if got != c.want {
				t.Fatalf("got:\n%s\n\nwant:\n%s", got, c.want)
			}
		})
	}
}

func TestParseSprintString(t *testing.T) {
	sp, ok := parseSprintString("com.atlassian.greenhopper.service.sprint.Sprint@14b1c359[id=7,rapidViewId=2,state=CLOSED,name=Sprint 7, the big one,goal=Ship it, finally,startDate=2024-01-01T09:00:00.000Z,endDate=2024-01-15T09:00:00.000Z,completeDate=<null>,sequence=7]")
	if !ok || sp.ID != 7 || sp.State != "closed" || sp.Name != "Sprint 7, the big one" || sp.Goal != "Ship it, finally" || sp.OriginBoardID != 2 {
		t.Fatalf("parsed %+v", sp)
	}
	if sp.StartDate.IsZero() || !sp.CompleteDate.IsZero() {
		t.Fatalf("dates %+v", sp)
	}
	if !strings.HasPrefix(sp.StartDate.Format("2006-01-02T15"), "2024-01-01T09") {
		t.Fatalf("start %v", sp.StartDate)
	}
}
