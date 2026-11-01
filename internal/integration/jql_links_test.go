package integration

import (
	"net/url"
	"testing"
)

func TestJQLLinksAndEpics(t *testing.T) {
	e := newEnv(t)
	c := e.login("ada@example.com", "Ada")
	c.createProject("LNK", "scrum")
	key := func(i obj) string { return str(i, "key") }
	epic := key(c.createIssue("LNK", "epic", "Checkout", nil))
	story := key(c.createIssue("LNK", "story", "Pay by card", obj{"parentKey": epic}))
	sub := key(c.createIssue("LNK", "subtask", "Card form", obj{"parentKey": story}))
	blocker := key(c.createIssue("LNK", "task", "Payment provider contract", nil))
	related := key(c.createIssue("LNK", "task", "Fraud checks", nil))
	dup := key(c.createIssue("LNK", "bug", "Card form is slow (dup)", nil))
	link := func(from, typ, to string) {
		c.must("POST", "/api/v1/issues/"+from+"/links", obj{"type": typ, "targetKey": to}, 201, nil)
	}
	link(blocker, "blocks", story)
	link(related, "relates", story)
	link(dup, "duplicates", sub)

	// epic = the epic ancestor, so sub-tasks count; parent is the direct parent only.
	sameStrings(t, c.search(`epic = `+epic+` ORDER BY key`), []string{story, sub}, "epic includes sub-tasks")
	sameStrings(t, c.search(`parent = `+epic), []string{story}, "parent is direct")
	sameStrings(t, c.search(`project = LNK AND epic is EMPTY AND level = 0 ORDER BY key`), []string{blocker, related, dup}, "no epic")

	// Links.
	sameStrings(t, c.search(`issue in linkedIssues("`+story+`") ORDER BY key`), []string{blocker, related}, "linked either way")
	sameStrings(t, c.search(`issue in linkedIssues("`+story+`", "is blocked by")`), []string{blocker}, "what blocks the story")
	sameStrings(t, c.search(`issue in linkedIssues("`+blocker+`", "blocks")`), []string{story}, "what the blocker blocks")
	sameStrings(t, c.search(`issueLinkType = "is blocked by"`), []string{story}, "issues that are blocked")
	sameStrings(t, c.search(`project = LNK AND issueLinkType = duplicates`), []string{dup}, "direction matters")
	sameStrings(t, c.search(`project = LNK AND issueLinkType is EMPTY ORDER BY key`), []string{epic}, "no links")

	// Blocked by unfinished work only.
	sameStrings(t, c.search(`issue in blockedIssues()`), []string{story}, "blocked while the blocker is open")
	sameStrings(t, c.search(`issue in blockingIssues()`), []string{blocker}, "blocking")
	c.transition(blocker, "start-work", 200)
	c.transition(blocker, "done", 200)
	sameStrings(t, c.search(`issue in blockedIssues()`), nil, "unblocked once the blocker is done")
	sameStrings(t, c.search(`issue not in blockedIssues() AND project = LNK AND level = 0 ORDER BY key`), []string{story, blocker, related, dup}, "negation")

	c.must("GET", `/api/v1/search?jql=`+url.QueryEscape(`issue in linkedIssues()`), nil, 400, nil)
	c.must("GET", `/api/v1/search?jql=`+url.QueryEscape(`issueLinkType = "frobs"`), nil, 400, nil)
}

// Issue keys in a query resolve only within projects the caller can browse: otherwise
// linkedIssues("SEC-1") would show which visible issues link to an issue they can't see.
func TestJQLKeysRespectBrowse(t *testing.T) {
	e := newEnv(t)
	ada := e.login("ada@example.com", "Ada")
	bob := e.login("bob@example.com", "Bob")
	ada.createProject("OPEN", "scrum")
	ada.must("POST", "/api/v1/projects", obj{"key": "SEC", "name": "Secret", "template": "scrum", "access": "private"}, 201, nil)
	key := func(i obj) string { return str(i, "key") }
	secret := key(ada.createIssue("SEC", "task", "Acquire Initech", nil))
	secretEpic := key(ada.createIssue("SEC", "epic", "Merger", nil))
	visible := key(ada.createIssue("OPEN", "task", "Rename the billing service", nil))
	ada.must("POST", "/api/v1/issues/"+visible+"/links", obj{"type": "blocks", "targetKey": secret}, 201, nil)

	queries := []string{
		`issue in linkedIssues("` + secret + `")`,
		`issue in linkedIssues("` + secret + `", "blocks")`,
		`parent = ` + secretEpic,
		`epic = ` + secretEpic,
		`key = ` + secret + ` OR issue in linkedIssues("` + secret + `")`,
	}
	for _, q := range queries {
		sameStrings(t, bob.search(q), []string{}, "bob: "+q)
	}
	// Ada, an org admin, can browse both projects.
	sameStrings(t, ada.search(queries[0]), []string{visible}, "ada sees the link")

	// A member of the secret project sees it again.
	ada.must("POST", "/api/v1/projects/SEC/members", obj{"userId": bob.id(), "roleKey": "viewer"}, 201, nil)
	sameStrings(t, bob.search(queries[0]), []string{visible}, "bob as a member")
}
