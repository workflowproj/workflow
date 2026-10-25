package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/workflowproj/workflow/internal/authz"
)

// ---------------------------------------------------------------------------
// Helpers for the service desk

// serviceDesk creates a service project with an agent (project member) besides its admin.
func serviceDesk(t *testing.T, e *Env, key string) (admin, agent *Client) {
	t.Helper()
	admin = e.login("ada@example.com", "Ada Admin")
	agent = e.login("aaron@example.com", "Aaron Agent")
	var p obj
	admin.must("POST", "/api/v1/projects", obj{"key": key, "name": key + " Help", "template": "service"}, 201, &p)
	eq(t, str(p, "template"), "service", "template")
	admin.must("POST", "/api/v1/projects/"+key+"/members", obj{"userId": agent.id(), "roleKey": "member"}, 201, nil)
	return admin, agent
}

// addCustomer puts a customer on a desk and signs them in (development sign-in keeps the
// customer role of an existing account).
func (c *Client) addCustomer(key, email, name string) *Client {
	c.e.t.Helper()
	var added obj
	c.must("POST", "/api/v1/projects/"+key+"/customers", obj{"email": email, "displayName": name}, 201, &added)
	cust := c.e.login(email, name)
	eq(c.e.t, str(cust.Me, "orgRole"), "customer", "signed in as a customer")
	return cust
}

func (c *Client) requestType(key, name string) obj {
	c.e.t.Helper()
	var rts []obj
	c.get("/api/v1/projects/"+key+"/request-types", &rts)
	for _, rt := range rts {
		if str(rt, "name") == name {
			return rt
		}
	}
	c.e.t.Fatalf("no request type %q in %s", name, key)
	return nil
}

func (c *Client) raise(key, requestTypeID string, values obj) obj {
	c.e.t.Helper()
	var r obj
	c.must("POST", "/api/v1/portal/projects/"+key+"/requests", obj{"requestTypeId": requestTypeID, "values": values}, 201, &r)
	return r
}

func (c *Client) serviceRequest(key string) obj {
	c.e.t.Helper()
	var r obj
	c.get("/api/v1/issues/"+key+"/request", &r)
	return r
}

func slaOf(r obj, metric string) obj {
	for _, s := range r["slas"].([]any) {
		if m := s.(map[string]any); m["metric"] == metric {
			return m
		}
	}
	return nil
}

func commentBodies(r obj) []string {
	var out []string
	for _, c := range r["comments"].([]any) {
		out = append(out, c.(map[string]any)["body"].(string))
	}
	return out
}

func (c *Client) transitionTo(key, transitionID string, extra obj) {
	c.e.t.Helper()
	body := obj{"transitionId": transitionID}
	for k, v := range extra {
		body[k] = v
	}
	c.must("POST", "/api/v1/issues/"+key+"/transitions", body, 200, nil)
}

func statusName(r obj) string { return str(r, "status", "name") }

// mailText joins every email sent to an address (subjects and bodies).
func (e *Env) mailText(addr string) string {
	var b strings.Builder
	for _, m := range e.mailTo(addr) {
		b.WriteString(m.Subject + "\n" + m.Text + "\n" + m.HTML + "\n")
	}
	return b.String()
}

func (c *Client) upload(path, filename, content string) Response {
	c.e.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("file", filename)
	_, _ = part.Write([]byte(content))
	_ = w.Close()
	return c.do("POST", path, buf.Bytes(), "Content-Type", w.FormDataContentType())
}

// ---------------------------------------------------------------------------

func TestServiceProjectSetUp(t *testing.T) {
	e := newEnv(t)
	admin, agent := serviceDesk(t, e, "HELP")

	var desk obj
	agent.get("/api/v1/projects/HELP/service-desk", &desk)
	eq(t, str(desk, "portalName"), "HELP Help", "portal named after the project")
	eq(t, str(desk, "portalAccess"), "customers", "customers only by default")
	eq(t, desk["selfSignup"], any(false), "self sign-up is off by default")
	eq(t, str(desk, "portalUrl"), e.Cfg.PublicURL+"/portal/HELP", "portal URL")

	var types []obj
	agent.get("/api/v1/projects/HELP/request-types", &types)
	if len(types) < 3 {
		t.Fatalf("starter request types: %v", types)
	}
	get := admin.requestType("HELP", "Get IT help")
	fields := get["fields"].([]any)
	eq(t, fields[0].(map[string]any)["field"], any("summary"), "summary first")
	eq(t, fields[0].(map[string]any)["type"], any("text"), "typed")

	var queues []obj
	agent.get("/api/v1/projects/HELP/queues", &queues)
	var names []string
	for _, q := range queues {
		names = append(names, str(q, "name"))
	}
	for _, want := range []string{"Unassigned", "Assigned to me", "Waiting for customer", "SLA at risk", "SLA breached"} {
		if !slices.Contains(names, want) {
			t.Fatalf("default queues %v lack %q", names, want)
		}
	}
	var cfg obj
	agent.get("/api/v1/projects/HELP/slas", &cfg)
	eq(t, len(cfg["calendars"].([]any)), 1, "a business-hours calendar")
	eq(t, len(cfg["metrics"].([]any)), 2, "first response and resolution")
	first := cfg["metrics"].([]any)[0].(map[string]any)
	if !slices.Contains(toStrings(first["pauseStatuses"]), "Waiting for customer") {
		t.Fatalf("pause statuses: %v", first["pauseStatuses"])
	}

	// Agents cannot change the settings; project admins can.
	agent.must("PATCH", "/api/v1/projects/HELP/service-desk", obj{"portalName": "IT"}, 403, nil)
	admin.must("PATCH", "/api/v1/projects/HELP/service-desk", obj{"portalName": "IT help", "portalAccess": "organisation"}, 200, &desk)
	eq(t, str(desk, "portalName"), "IT help", "renamed")

	// Software projects are unaffected: no service desk, no internal notes.
	admin.createProject("SOFT", "kanban")
	admin.must("GET", "/api/v1/projects/SOFT/service-desk", nil, 404, nil)
	admin.createIssue("SOFT", "task", "Plain task", nil)
	var c obj
	admin.must("POST", "/api/v1/issues/SOFT-1/comments", obj{"body": "hello"}, 201, &c)
	if _, ok := c["visibility"]; ok {
		t.Fatalf("software comments have no visibility: %v", c)
	}
	admin.must("POST", "/api/v1/issues/SOFT-1/comments", obj{"body": "secret", "visibility": "internal"}, 400, nil)
	var hist []obj
	admin.get("/api/v1/issues/SOFT-1/history", &hist)
	for _, h := range hist {
		if str(h, "field") == "comment" {
			t.Fatal("software comments are not recorded in history")
		}
	}
	admin.must("GET", "/api/v1/issues/SOFT-1/request", nil, 404, nil)
}

func TestRequestTypeForms(t *testing.T) {
	e := newEnv(t)
	admin, agent := serviceDesk(t, e, "HELP")
	admin.must("POST", "/api/v1/fields", obj{"key": "laptop_model", "name": "Laptop model", "type": "select", "options": []string{"13-inch", "15-inch"}}, 201, nil)
	admin.must("POST", "/api/v1/fields", obj{"key": "manager", "name": "Manager", "type": "user"}, 201, nil)

	in := obj{
		"name": "New laptop", "issueTypeKey": "service_request", "icon": "laptop", "group": "Hardware",
		"fields": []obj{
			{"field": "summary", "label": "What for?", "required": true},
			{"field": "laptop_model", "label": "Model", "help": "Pick one", "required": true},
			{"field": "dueDate", "label": "Needed by"},
		},
	}
	agent.must("POST", "/api/v1/projects/HELP/request-types", in, 403, nil)
	var rt obj
	admin.must("POST", "/api/v1/projects/HELP/request-types", in, 201, &rt)
	f := rt["fields"].([]any)[1].(map[string]any)
	eq(t, f["type"], any("select"), "custom select field")
	eq(t, len(f["options"].([]any)), 2, "options")

	// Validation: user fields would show customers the project's people; unknown fields; approvals.
	bad := []obj{
		{"name": "X", "issueTypeKey": "service_request", "fields": []obj{{"field": "manager", "label": "Manager", "required": false}}},
		{"name": "X", "issueTypeKey": "service_request", "fields": []obj{{"field": "nope", "label": "Nope", "required": false}}},
		{"name": "X", "issueTypeKey": "service_request", "fields": []obj{{"field": "summary", "label": "A", "required": false}, {"field": "summary", "label": "B", "required": false}}},
		{"name": "X", "issueTypeKey": "nope"},
		{"name": "", "issueTypeKey": "service_request"},
		{"name": "X", "issueTypeKey": "service_request", "icon": "rocket"},
		{"name": "X", "issueTypeKey": "service_request", "approval": obj{"mode": "any", "approvedStatus": "Waiting for support", "declinedStatus": "Declined"}},
		{"name": "X", "issueTypeKey": "service_request", "approval": obj{"mode": "some", "approverIds": []string{agent.id()}, "approvedStatus": "Waiting for support", "declinedStatus": "Declined"}},
		{"name": "X", "issueTypeKey": "service_request", "approval": obj{"mode": "any", "approverIds": []string{agent.id()}, "approvedStatus": "Nowhere", "declinedStatus": "Declined"}},
		{"name": "X", "issueTypeKey": "service_request", "approval": obj{"mode": "any", "approverIds": []string{agent.id()}, "approvedStatus": "Closed", "declinedStatus": "Declined"}},
	}
	for k, b := range bad {
		if r := admin.do("POST", "/api/v1/projects/HELP/request-types", b); r.Status != 400 {
			t.Fatalf("bad request type %d: %d %s", k, r.Status, r.Body)
		}
	}
	admin.must("POST", "/api/v1/projects/HELP/request-types", in, 409, nil) // duplicate name

	// The portal shows the form; values are checked against it.
	cust := admin.addCustomer("HELP", "carol@customer.test", "Carol Customer")
	var desk obj
	cust.get("/api/v1/portal/projects/HELP", &desk)
	var portalType obj
	for _, x := range desk["requestTypes"].([]any) {
		if m := x.(map[string]any); m["name"] == "New laptop" {
			portalType = m
		}
	}
	if portalType == nil || portalType["group"] != "Hardware" {
		t.Fatalf("portal request types: %v", desk["requestTypes"])
	}
	id := str(rt, "id")
	cust.must("POST", "/api/v1/portal/projects/HELP/requests", obj{"requestTypeId": id, "values": obj{"summary": "For Bo"}}, 400, nil)
	cust.must("POST", "/api/v1/portal/projects/HELP/requests", obj{"requestTypeId": id, "values": obj{"summary": "For Bo", "laptop_model": "17-inch"}}, 400, nil)
	cust.must("POST", "/api/v1/portal/projects/HELP/requests", obj{"requestTypeId": id, "values": obj{"summary": "For Bo", "laptop_model": "13-inch", "priority": "high"}}, 400, nil)
	cust.must("POST", "/api/v1/portal/projects/HELP/requests", obj{"requestTypeId": id, "values": obj{"summary": "For Bo", "laptop_model": "13-inch", "dueDate": "soon"}}, 400, nil)
	r := cust.raise("HELP", id, obj{"summary": "For Bo", "laptop_model": "13-inch", "dueDate": "2026-12-01"})
	eq(t, str(r, "requestType", "name"), "New laptop", "request type")
	labels := map[string]string{}
	for _, f := range r["fields"].([]any) {
		m := f.(map[string]any)
		labels[m["label"].(string)] = m["value"].(string)
	}
	eq(t, labels["Model"], "13-inch", "custom field value shown")
	eq(t, labels["Needed by"], "2026-12-01", "due date shown")
	issue := agent.issue(str(r, "key"))
	eq(t, str(issue, "fields", "laptop_model"), "13-inch", "stored as the custom field")
	eq(t, str(issue, "reporter", "id"), cust.id(), "reported by the customer")

	// A request type in use cannot be deleted; an unused one can.
	admin.must("DELETE", "/api/v1/request-types/"+id, nil, 409, nil)
	var other obj
	admin.must("POST", "/api/v1/projects/HELP/request-types", obj{"name": "Spare", "issueTypeKey": "incident"}, 201, &other)
	admin.must("DELETE", "/api/v1/request-types/"+str(other, "id"), nil, 204, nil)
}

func TestPortalConversation(t *testing.T) {
	e := newEnv(t)
	admin, agent := serviceDesk(t, e, "HELP")

	// The agent invites a customer: a customer account and an invitation to choose a password.
	var added obj
	agent.must("POST", "/api/v1/projects/HELP/customers", obj{"email": "Carol@Customer.test", "displayName": "Carol Customer"}, 201, &added)
	eq(t, added["invited"], any(true), "invited")
	eq(t, str(added, "customer", "account"), "customer", "a customer account")
	msgs := e.mailTo("carol@customer.test")
	eq(t, len(msgs), 1, "invitation email")
	if !strings.Contains(msgs[0].Text, "HELP Help help centre") || !strings.Contains(msgs[0].Text, "Aaron Agent invited you") {
		t.Fatalf("invitation:\n%s", msgs[0].Text)
	}
	token := linkToken(t, msgs[0], "invite")
	carol := e.anon()
	carol.must("POST", "/api/v1/auth/local/link/redeem", obj{"token": token, "password": "Copper-Lantern-Harbour-71"}, 200, nil)
	wantStatus(t, carol.passwordLogin("carol@customer.test", "Copper-Lantern-Harbour-71"), 200, "signed_in")
	var me obj
	carol.get("/api/v1/me", &me)
	eq(t, str(me, "orgRole"), "customer", "customer role")

	// Help centre → request type → form → request.
	var desks []obj
	carol.get("/api/v1/portal/projects", &desks)
	eq(t, len(desks), 1, "one desk")
	rt := agent.requestType("HELP", "Get IT help")
	r := carol.raise("HELP", str(rt, "id"), obj{"summary": "VPN keeps dropping", "description": "Since **Monday**."})
	key := str(r, "key")
	eq(t, key, "HELP-1", "key")
	eq(t, statusName(r), "Waiting for support", "initial status")
	eq(t, str(r, "role"), "reporter", "role")
	if !strings.Contains(e.mailText("carol@customer.test"), "We received your request HELP-1") {
		t.Fatalf("request received email missing:\n%s", e.mailText("carol@customer.test"))
	}

	// Agents see the request type and both SLA clocks running.
	sr := agent.serviceRequest(key)
	eq(t, str(sr, "requestType", "name"), "Get IT help", "request type")
	eq(t, sr["customerVisible"], any(true), "shown in the portal")
	eq(t, str(slaOf(sr, "first_response"), "state"), "running", "first response running")
	eq(t, str(slaOf(sr, "resolution"), "state"), "running", "resolution running")

	// An internal note first: the first-response clock keeps running.
	var note obj
	agent.must("POST", "/api/v1/issues/"+key+"/comments", obj{"body": "INTERNAL: probably the MTU again"}, 201, &note)
	eq(t, str(note, "visibility"), "internal", "agents' comments are internal by default")
	eq(t, str(slaOf(agent.serviceRequest(key), "first_response"), "state"), "running", "a note is not a response")
	// A reply to the customer stops it.
	var reply obj
	agent.must("POST", "/api/v1/issues/"+key+"/comments", obj{"body": "Could you try the new client?", "visibility": "public"}, 201, &reply)
	eq(t, str(reply, "visibility"), "public", "public reply")
	first := slaOf(agent.serviceRequest(key), "first_response")
	eq(t, str(first, "state"), "stopped", "first response met")
	eq(t, first["breached"], any(false), "in time")

	// Agents see both, labelled; the history and MCP say which is which.
	var comments []obj
	agent.get("/api/v1/issues/"+key+"/comments", &comments)
	eq(t, len(comments), 2, "agents see the note and the reply")
	var hist []obj
	agent.get("/api/v1/issues/"+key+"/history", &hist)
	var commentHistory []string
	for _, h := range hist {
		if str(h, "field") == "comment" {
			commentHistory = append(commentHistory, str(h, "to"))
		}
	}
	sameStrings(t, commentHistory, []string{"Internal note", "Reply to customer"}, "comment visibility in history")

	// The customer sees only the reply.
	var view obj
	carol.get("/api/v1/portal/requests/"+key, &view)
	sameStrings(t, commentBodies(view), []string{"Could you try the new client?"}, "public conversation only")
	if strings.Contains(string(carol.do("GET", "/api/v1/portal/requests/"+key, nil).Body), "MTU") {
		t.Fatal("the internal note reached the portal")
	}
	mail := e.mailText("carol@customer.test")
	if !strings.Contains(mail, "Aaron Agent replied on HELP-1") || !strings.Contains(mail, "Could you try the new client?") {
		t.Fatalf("reply email missing:\n%s", mail)
	}
	if strings.Contains(mail, "MTU") {
		t.Fatal("the internal note was emailed to the customer")
	}

	// Waiting for the customer pauses the resolution clock and emails them; their reply hands
	// the request back to support.
	agent.transitionTo(key, "wait-for-customer", nil)
	eq(t, str(slaOf(agent.serviceRequest(key), "resolution"), "state"), "paused", "paused while waiting")
	if !strings.Contains(e.mailText("carol@customer.test"), "HELP-1 needs your reply") {
		t.Fatal("waiting-for-customer email missing")
	}
	var cc obj
	carol.must("POST", "/api/v1/portal/requests/"+key+"/comments", obj{"body": "The new client works, thanks!"}, 201, &cc)
	carol.get("/api/v1/portal/requests/"+key, &view)
	eq(t, statusName(view), "Waiting for support", "back to support")
	eq(t, str(slaOf(agent.serviceRequest(key), "resolution"), "state"), "running", "running again")
	agent.get("/api/v1/issues/"+key+"/comments", &comments)
	eq(t, str(comments[2], "source"), "portal", "written in the portal")
	eq(t, str(comments[2], "visibility"), "public", "portal replies are public")

	// Files: the customer's own attachment is shown; an agent's is not.
	up := carol.upload("/api/v1/portal/requests/"+key+"/attachments", "screenshot.txt", "pixels")
	eq(t, up.Status, 201, "portal upload")
	agentUp := agent.upload("/api/v1/issues/"+key+"/attachments", "internal-runbook.txt", "secret steps")
	eq(t, agentUp.Status, 201, "agent upload")
	carol.get("/api/v1/portal/requests/"+key, &view)
	eq(t, len(view["attachments"].([]any)), 1, "only the portal file")
	att := view["attachments"].([]any)[0].(map[string]any)
	got := carol.do("GET", att["url"].(string), nil)
	eq(t, got.Status, 200, "download")
	eq(t, string(got.Body), "pixels", "content")
	eq(t, carol.do("GET", "/api/v1/portal/attachments/"+str(agentUp.JSON(), "id"), nil).Status, 404, "agents' files are not downloadable from the portal")
	detail := agent.issue(key)
	for _, a := range detail["attachments"].([]any) {
		m := a.(map[string]any)
		want := map[string]string{"screenshot.txt": "public", "internal-runbook.txt": "internal"}[m["filename"].(string)]
		eq(t, m["visibility"], any(want), "attachment visibility for agents")
	}

	// Resolving emails the customer; the resolution clock stops.
	agent.transitionTo(key, "resolve", obj{"fields": obj{"resolution": "Done"}, "comment": "Closing: MTU fixed on the gateway"})
	sr = agent.serviceRequest(key)
	eq(t, str(slaOf(sr, "resolution"), "state"), "stopped", "resolution stopped")
	mail = e.mailText("carol@customer.test")
	if !strings.Contains(mail, "HELP-1 is resolved") {
		t.Fatalf("resolved email missing:\n%s", mail)
	}
	if strings.Contains(mail, "MTU fixed") {
		t.Fatal("a transition comment (internal by default) was emailed")
	}
	carol.get("/api/v1/portal/requests/"+key, &view)
	eq(t, len(view["comments"].([]any)), 2, "the transition's internal note stays internal")

	// My requests: open and closed.
	var mine []obj
	carol.get("/api/v1/portal/requests?status=closed", &mine)
	eq(t, len(mine), 1, "closed")
	carol.get("/api/v1/portal/requests?status=open", &mine)
	eq(t, len(mine), 0, "none open")

	// The customer's email settings: only request kinds, immediately or off.
	var settings obj
	carol.get("/api/v1/me/email-notifications", &settings)
	var kinds []string
	for _, p := range settings["preferences"].([]any) {
		kinds = append(kinds, p.(map[string]any)["kind"].(string))
	}
	sameStrings(t, kinds, []string{"requests", "approvals"}, "portal kinds only")
	carol.must("PATCH", "/api/v1/me/email-notifications", obj{"preferences": []obj{{"kind": "requests", "delivery": "digest"}}}, 400, nil)
	carol.must("PATCH", "/api/v1/me/email-notifications", obj{"preferences": []obj{{"kind": "requests", "delivery": "off"}}}, 200, nil)
	before := len(e.mailTo("carol@customer.test"))
	agent.transitionTo(key, "reopen", nil)
	agent.must("POST", "/api/v1/issues/"+key+"/comments", obj{"body": "Reopened to double-check", "visibility": "public"}, 201, nil)
	eq(t, len(e.mailTo("carol@customer.test")), before, "no email once switched off")
	_ = admin
}

func TestApprovals(t *testing.T) {
	e := newEnv(t)
	admin, agent := serviceDesk(t, e, "HELP")
	carol := admin.addCustomer("HELP", "carol@customer.test", "Carol Customer")
	dave := admin.addCustomer("HELP", "dave@customer.test", "Dave Manager")
	erin := e.login("erin@example.com", "Erin Finance")

	rt := admin.requestType("HELP", "Request new hardware")
	var cfg obj
	admin.must("PUT", "/api/v1/request-types/"+str(rt, "id"), obj{
		"name": "Request new hardware", "issueTypeKey": "service_request", "icon": "laptop",
		"fields":   []obj{{"field": "summary", "label": "What do you need?", "required": true}},
		"approval": obj{"mode": "all", "approverIds": []string{dave.id(), erin.id()}, "approvedStatus": "Waiting for support", "declinedStatus": "Declined"},
	}, 200, &cfg)
	eq(t, len(cfg["approval"].(map[string]any)["approvers"].([]any)), 2, "approvers")

	r := carol.raise("HELP", str(rt, "id"), obj{"summary": "A 4K monitor"})
	key := str(r, "key")
	eq(t, statusName(r), "Pending approval", "waits for the approval")
	eq(t, len(r["approvals"].([]any)), 1, "an approval")
	eq(t, r["approvals"].([]any)[0].(map[string]any)["canDecide"], any(false), "the reporter is not an approver")
	if !strings.Contains(e.mailText("dave@customer.test"), "Your approval is needed on "+key) || !strings.Contains(e.mailText("erin@example.com"), "Your approval is needed on "+key) {
		t.Fatal("approval emails missing")
	}
	// The resolution clock pauses while the request waits for its approval.
	eq(t, str(slaOf(agent.serviceRequest(key), "resolution"), "state"), "paused", "paused pending approval")
	// Agents cannot skip the approval through the workflow.
	var trs []obj
	agent.get("/api/v1/issues/"+key+"/transitions", &trs)
	for _, tr := range trs {
		if id := str(tr, "id"); id == "approve" || id == "decline" {
			t.Fatalf("agents are offered %q", id)
		}
	}
	agent.transition(key, "approve", 403)

	// The approver sees it in the portal (without being a customer of the request).
	var pending []obj
	dave.get("/api/v1/portal/approvals", &pending)
	sameStrings(t, keys(pending), []string{key}, "waiting for Dave")
	var view obj
	dave.get("/api/v1/portal/requests/"+key, &view)
	eq(t, str(view, "role"), "approver", "approver role")
	eq(t, view["canComment"], any(false), "approvers decide; they do not reply")
	dave.must("POST", "/api/v1/portal/requests/"+key+"/comments", obj{"body": "hi"}, 403, nil)
	carol.must("POST", "/api/v1/portal/requests/"+key+"/approval", obj{"decision": "approve"}, 403, nil)

	// All must approve: Dave first (portal), then Erin (agent issue view).
	dave.must("POST", "/api/v1/portal/requests/"+key+"/approval", obj{"decision": "approve", "comment": "Fine by me"}, 200, &view)
	eq(t, statusName(view), "Pending approval", "still waiting for Erin")
	dave.must("POST", "/api/v1/portal/requests/"+key+"/approval", obj{"decision": "approve"}, 403, nil)
	approvalID := str(agent.serviceRequest(key)["approvals"].([]any)[0].(map[string]any), "id")
	agent.must("POST", "/api/v1/issues/"+key+"/approvals/"+approvalID, obj{"decision": "approve"}, 403, nil)
	erin.must("POST", "/api/v1/issues/"+key+"/approvals/"+approvalID, obj{"decision": "approve"}, 404, nil) // Erin is no agent: the issue is not hers to browse
	var erinView obj
	erin.must("POST", "/api/v1/portal/requests/"+key+"/approval", obj{"decision": "approve"}, 200, &erinView)
	eq(t, statusName(erinView), "Waiting for support", "approved: back to support")
	appr := erinView["approvals"].([]any)[0].(map[string]any)
	eq(t, appr["status"], any("approved"), "approved")
	eq(t, str(slaOf(agent.serviceRequest(key), "resolution"), "state"), "running", "running once approved")
	var hist []obj
	agent.get("/api/v1/issues/"+key+"/history", &hist)
	var approvals []string
	for _, h := range hist {
		if str(h, "field") == "approval" {
			approvals = append(approvals, str(h, "to"))
		}
	}
	sameStrings(t, approvals, []string{"Approval requested from Dave Manager, Erin Finance (all must approve)", "Dave Manager approved: Fine by me", "Erin Finance approved", "Request approved"}, "approval history")
	last := hist[len(hist)-1]
	eq(t, str(last, "field"), "status", "then the status change")
	eq(t, str(last, "via"), "approval", "made by the approval")
	eq(t, str(last, "actor", "displayName"), "Erin Finance", "as the last approver")

	// A decline (any one is enough) closes the request as Declined, resolution Won't Do.
	admin.must("PUT", "/api/v1/request-types/"+str(rt, "id"), obj{
		"name": "Request new hardware", "issueTypeKey": "service_request",
		"fields":   []obj{{"field": "summary", "label": "What do you need?", "required": true}},
		"approval": obj{"mode": "any", "approverIds": []string{dave.id(), agent.id()}, "approvedStatus": "Waiting for support", "declinedStatus": "Declined"},
	}, 200, nil)
	r2 := carol.raise("HELP", str(rt, "id"), obj{"summary": "A second monitor"})
	k2 := str(r2, "key")
	id2 := str(agent.serviceRequest(k2)["approvals"].([]any)[0].(map[string]any), "id")
	agent.must("POST", "/api/v1/issues/"+k2+"/approvals/"+id2, obj{"decision": "decline", "comment": "Over budget"}, 200, nil)
	issue := agent.issue(k2)
	eq(t, str(issue, "status", "name"), "Declined", "declined")
	eq(t, str(issue, "resolution"), "Won't Do", "resolution")
	dave.must("POST", "/api/v1/portal/requests/"+k2+"/approval", obj{"decision": "approve"}, 403, nil)
	var carolView obj
	carol.get("/api/v1/portal/requests/"+k2, &carolView)
	ap := carolView["approvals"].([]any)[0].(map[string]any)
	eq(t, ap["status"], any("declined"), "the customer sees the decision")
	var reason string
	for _, a := range ap["approvers"].([]any) {
		if c, ok := a.(map[string]any)["comment"]; ok {
			reason = c.(string)
		}
	}
	eq(t, reason, "Over budget", "and the reason")
	if !strings.Contains(e.mailText("carol@customer.test"), k2+" is declined") {
		t.Fatal("declined email missing")
	}
}

func TestSLAs(t *testing.T) {
	e := newEnv(t)
	admin, agent := serviceDesk(t, e, "HELP")
	carol := admin.addCustomer("HELP", "carol@customer.test", "Carol Customer")
	rt := admin.requestType("HELP", "Report a problem")

	// Around the clock: high priority incidents get a first response in 30 minutes; the rest in
	// 2 hours. Resolution: 8 hours of a business-hours calendar.
	cfg := obj{
		"calendars": []obj{{"name": "Office", "timeZone": "Europe/London", "hours": []obj{
			{"day": 1, "start": "09:00", "end": "17:00"}, {"day": 2, "start": "09:00", "end": "17:00"}, {"day": 3, "start": "09:00", "end": "17:00"},
			{"day": 4, "start": "09:00", "end": "17:00"}, {"day": 5, "start": "09:00", "end": "17:00"},
		}, "holidays": []string{"2026-12-25"}}},
		"metrics": []obj{
			{"metric": "first_response", "name": "Time to first response", "pauseStatuses": []string{"Waiting for customer"}, "goals": []obj{
				{"priorities": []string{"highest", "high"}, "requestTypeIds": []string{str(rt, "id")}, "targetMinutes": 30},
				{"targetMinutes": 120},
			}},
			{"metric": "resolution", "name": "Time to resolution", "pauseStatuses": []string{"Waiting for customer", "Pending approval"}, "goals": []obj{
				{"targetMinutes": 480, "calendar": "Office"},
			}},
		},
	}
	agent.must("PUT", "/api/v1/projects/HELP/slas", cfg, 403, nil)
	for _, bad := range []obj{
		{"calendars": []obj{{"name": "X", "timeZone": "Mars/Base", "hours": []obj{}, "holidays": []string{}}}, "metrics": []obj{}},
		{"calendars": []obj{{"name": "X", "timeZone": "UTC", "hours": []obj{{"day": 1, "start": "17:00", "end": "09:00"}}, "holidays": []string{}}}, "metrics": []obj{}},
		{"calendars": []obj{}, "metrics": []obj{{"metric": "speed", "name": "x", "pauseStatuses": []string{}, "goals": []obj{}}}},
		{"calendars": []obj{}, "metrics": []obj{{"metric": "resolution", "name": "x", "pauseStatuses": []string{"Nope"}, "goals": []obj{}}}},
		{"calendars": []obj{}, "metrics": []obj{{"metric": "resolution", "name": "x", "pauseStatuses": []string{}, "goals": []obj{{"targetMinutes": 10, "calendar": "Missing"}}}}},
		{"calendars": []obj{}, "metrics": []obj{{"metric": "resolution", "name": "x", "pauseStatuses": []string{}, "goals": []obj{{"targetMinutes": 10, "jql": "status = "}}}}},
	} {
		if r := admin.do("PUT", "/api/v1/projects/HELP/slas", bad); r.Status != 400 {
			t.Fatalf("bad SLA config accepted: %d %s", r.Status, r.Body)
		}
	}
	var saved obj
	admin.must("PUT", "/api/v1/projects/HELP/slas", cfg, 200, &saved)
	eq(t, len(saved["calendars"].([]any)), 1, "the old calendar went")
	eq(t, str(saved["calendars"].([]any)[0].(map[string]any), "name"), "Office", "calendar")

	urgent := carol.raise("HELP", str(rt, "id"), obj{"summary": "Email is down", "priority": "high"})
	normal := carol.raise("HELP", str(rt, "id"), obj{"summary": "Printer jams"})
	eq(t, slaOf(agent.serviceRequest(str(urgent, "key")), "first_response")["targetSeconds"], any(1800.0), "the high-priority goal")
	eq(t, slaOf(agent.serviceRequest(str(normal, "key")), "first_response")["targetSeconds"], any(7200.0), "the default goal")
	res := slaOf(agent.serviceRequest(str(normal, "key")), "resolution")
	eq(t, str(res, "calendar"), "Office", "measured in office hours")

	// Make the urgent request an hour old: its first response is now breached.
	e.exec(`UPDATE issues SET created_at = now() - interval '61 minutes' WHERE id = $1`, uuid.MustParse(str(agent.issue(str(urgent, "key")), "id")))
	agent.must("PATCH", "/api/v1/issues/"+str(urgent, "key"), obj{"labels": []string{"email"}}, 200, nil) // any change recomputes
	first := slaOf(agent.serviceRequest(str(urgent, "key")), "first_response")
	eq(t, first["breached"], any(true), "breached")
	if first["remainingSeconds"].(float64) > -1700 {
		t.Fatalf("about 31 minutes over: %v", first["remainingSeconds"])
	}

	// JQL and queues find it.
	sameStrings(t, agent.search(`project = HELP AND "Time to first response" = breached()`), []string{str(urgent, "key")}, "breached()")
	sameStrings(t, agent.search(`project = HELP AND timeToFirstResponse = running() ORDER BY key`), []string{str(urgent, "key"), str(normal, "key")}, "running()")
	sameStrings(t, agent.search(`project = HELP AND "Time to first response" < remaining("3h") AND "Time to first response" > remaining("0m")`), []string{str(normal, "key")}, "remaining()")
	sameStrings(t, agent.search(`project = HELP AND "Request type" = "Report a problem" ORDER BY key`), []string{str(urgent, "key"), str(normal, "key")}, "request type")
	sameStrings(t, agent.search(`project = HELP ORDER BY "Time to first response" ASC`), []string{str(urgent, "key"), str(normal, "key")}, "ordered by breach time")
	var queues []obj
	agent.get("/api/v1/projects/HELP/queues", &queues)
	counts := map[string]float64{}
	var breachedQueue string
	for _, q := range queues {
		counts[str(q, "name")] = q["count"].(float64)
		if str(q, "name") == "SLA breached" {
			breachedQueue = str(q, "id")
		}
	}
	eq(t, counts["SLA breached"], 1.0, "breached queue")
	eq(t, counts["Unassigned"], 2.0, "unassigned queue")
	var qi obj
	agent.get("/api/v1/queues/"+breachedQueue+"/issues", &qi)
	item := qi["items"].([]any)[0].(map[string]any)
	eq(t, str(item, "issue", "key"), str(urgent, "key"), "queue item")
	eq(t, str(item, "requestType", "name"), "Report a problem", "with its request type")
	eq(t, len(item["slas"].([]any)), 2, "and its clocks")

	// The periodic check raises sla.breached once: the assignee is notified, automation can react.
	agent.must("PATCH", "/api/v1/issues/"+str(urgent, "key"), obj{"assigneeId": agent.id()}, 200, nil)
	e.DrainJobs()
	if err := e.App.RunJob(context.Background(), "sla_check", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	e.DrainJobs()
	var notes struct {
		Items []obj `json:"items"`
	}
	agent.get("/api/v1/notifications", &notes)
	breaches := 0
	for _, n := range notes.Items {
		if str(n, "kind") == "sla" {
			breaches++
			if !strings.Contains(str(n, "body"), "Time to first response breached") {
				t.Fatalf("breach notification: %v", n)
			}
		}
	}
	eq(t, breaches, 1, "one breach notification")
	if err := e.App.RunJob(context.Background(), "sla_check", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	e.DrainJobs()
	agent.get("/api/v1/notifications", &notes)
	n := 0
	for _, x := range notes.Items {
		if str(x, "kind") == "sla" {
			n++
		}
	}
	eq(t, n, 1, "a breach is raised once")

	// Changing the configuration recomputes open requests (in the background).
	cfg["metrics"].([]obj)[0]["goals"] = []obj{{"targetMinutes": 600}}
	admin.must("PUT", "/api/v1/projects/HELP/slas", cfg, 200, nil)
	e.DrainJobs()
	first = slaOf(agent.serviceRequest(str(urgent, "key")), "first_response")
	eq(t, first["targetSeconds"], any(36000.0), "new target")
	eq(t, first["breached"], any(false), "no longer breached")
}

// TestCustomersCannotReachAgentData proves the boundary between customers and agents: other
// customers' requests, internal notes (portal, agent API, search, live events, emails,
// attachments, history, MCP, webhooks), JQL and every agent endpoint.
func TestCustomersCannotReachAgentData(t *testing.T) {
	e := newEnv(t)
	admin, agent := serviceDesk(t, e, "HELP")
	carol := admin.addCustomer("HELP", "carol@customer.test", "Carol Customer")
	ed := admin.addCustomer("HELP", "ed@customer.test", "Ed Other")
	rt := admin.requestType("HELP", "Get IT help")
	r := carol.raise("HELP", str(rt, "id"), obj{"summary": "Carol's laptop", "description": "serial ABC"})
	key := str(r, "key")
	agent.must("POST", "/api/v1/issues/"+key+"/comments", obj{"body": "SECRET-NOTE do not tell the customer"}, 201, nil)
	agent.must("POST", "/api/v1/issues/"+key+"/comments", obj{"body": "Looking into it", "visibility": "public"}, 201, nil)
	carolFile := str(carol.upload("/api/v1/portal/requests/"+key+"/attachments", "mine.txt", "carol").JSON(), "id")
	agentFile := str(agent.upload("/api/v1/issues/"+key+"/attachments", "agent.txt", "SECRET-FILE").JSON(), "id")

	// Other customers' requests: not listed, not readable, not writable, files not downloadable.
	var list []obj
	ed.get("/api/v1/portal/requests", &list)
	eq(t, len(list), 0, "Ed sees none of Carol's requests")
	for _, path := range []string{"/api/v1/portal/requests/" + key, "/api/v1/portal/attachments/" + carolFile, "/api/v1/portal/attachments/" + agentFile} {
		eq(t, ed.do("GET", path, nil).Status, 404, "Ed: "+path)
	}
	eq(t, ed.do("POST", "/api/v1/portal/requests/"+key+"/comments", obj{"body": "hi"}).Status, 404, "Ed cannot reply")
	eq(t, ed.upload("/api/v1/portal/requests/"+key+"/attachments", "x.txt", "x").Status, 404, "Ed cannot attach")
	eq(t, ed.do("POST", "/api/v1/portal/requests/"+key+"/approval", obj{"decision": "approve"}).Status, 404, "Ed cannot approve")
	// By id as well as by key.
	issueID := str(agent.issue(key), "id")
	eq(t, ed.do("GET", "/api/v1/portal/requests/"+issueID, nil).Status, 404, "by id")
	// A request agents raised without a request type (an internal issue) is not in the portal,
	// even when its reporter is the customer.
	internal := agent.createIssue("HELP", "incident", "Internal follow-up for Carol", nil)
	agent.must("PATCH", "/api/v1/issues/"+str(internal, "key"), obj{"reporterId": carol.id()}, 200, nil)
	eq(t, carol.do("GET", "/api/v1/portal/requests/"+str(internal, "key"), nil).Status, 404, "no request type: not in the portal")
	carol.get("/api/v1/portal/requests", &list)
	sameStrings(t, keys(list), []string{key}, "Carol's list")
	// Desks they may not use are invisible.
	admin.must("POST", "/api/v1/projects", obj{"key": "HR", "name": "HR", "template": "service"}, 201, nil)
	var desks []obj
	carol.get("/api/v1/portal/projects", &desks)
	eq(t, len(desks), 1, "only HELP")
	eq(t, carol.do("GET", "/api/v1/portal/projects/HR", nil).Status, 404, "HR desk hidden")
	eq(t, carol.do("POST", "/api/v1/portal/projects/HR/requests", obj{"requestTypeId": str(rt, "id"), "values": obj{"summary": "x"}}).Status, 404, "cannot raise there")
	eq(t, carol.do("GET", "/api/v1/portal/projects/SOFT", nil).Status, 404, "unknown desk")

	// Internal notes and agents' files never reach the portal.
	body := string(carol.do("GET", "/api/v1/portal/requests/"+key, nil).Body)
	if strings.Contains(body, "SECRET-NOTE") || strings.Contains(body, "agent.txt") {
		t.Fatalf("portal leaks agent data: %s", body)
	}
	eq(t, carol.do("GET", "/api/v1/portal/attachments/"+agentFile, nil).Status, 404, "agent file")

	// Every agent endpoint refuses customers, whatever it would find.
	agentPaths := []struct{ method, path string }{
		{"GET", "/api/v1/issues/" + key}, {"GET", "/api/v1/issues/" + key + "/comments"}, {"GET", "/api/v1/issues/" + key + "/history"},
		{"GET", "/api/v1/issues/" + key + "/request"}, {"GET", "/api/v1/attachments/" + agentFile}, {"GET", "/api/v1/attachments/" + carolFile},
		{"GET", "/api/v1/search?jql=" + url.QueryEscape("project = HELP")}, {"GET", "/api/v1/search/fields"}, {"GET", "/api/v1/filters"},
		{"GET", "/api/v1/projects"}, {"GET", "/api/v1/projects/HELP"}, {"GET", "/api/v1/projects/HELP/queues"}, {"GET", "/api/v1/projects/HELP/customers"},
		{"GET", "/api/v1/users"}, {"GET", "/api/v1/groups"}, {"GET", "/api/v1/notifications"}, {"GET", "/api/v1/me/tokens"},
		{"GET", "/api/v1/events?project=HELP"}, {"GET", "/api/v1/timeline"}, {"GET", "/api/v1/dashboards"}, {"GET", "/api/v1/statuses"},
		{"POST", "/api/v1/issues"}, {"POST", "/api/v1/issues/" + key + "/comments"}, {"POST", "/api/v1/projects"}, {"POST", "/api/v1/me/tokens"},
		{"POST", "/api/v1/webhooks"}, {"GET", "/api/v1/dev/mailbox"}, {"GET", "/api/v1/metrics/summary"},
	}
	for _, p := range agentPaths {
		r := carol.do(p.method, p.path, obj{})
		if r.Status != 403 || str(r.JSON(), "code") != "portal_only" {
			t.Fatalf("customer %s %s: %d %s", p.method, p.path, r.Status, r.Body)
		}
	}
	// Their own account works.
	carol.get("/api/v1/me", nil)
	carol.get("/api/v1/me/email-notifications", nil)
	carol.get("/api/v1/me/security", nil)

	// Customers can never become agents by membership: an admin cannot add them to a project,
	// and a group mapped to an organisation role does not change theirs.
	admin.must("POST", "/api/v1/projects/HELP/members", obj{"userId": carol.id(), "roleKey": "member"}, 400, nil)
	var g obj
	admin.must("POST", "/api/v1/admin/groups", obj{"displayName": "Everyone", "orgRole": "member"}, 201, &g)
	admin.must("PUT", "/api/v1/admin/groups/"+str(g, "id")+"/members/"+carol.id(), nil, 204, nil)
	admin.must("POST", "/api/v1/projects/HELP/members", obj{"groupId": str(g, "id"), "roleKey": "admin"}, 201, nil)
	eq(t, carol.do("GET", "/api/v1/projects", nil).Status, 403, "still a customer")
	eq(t, carol.do("GET", "/api/v1/issues/"+key, nil).Status, 403, "still a customer")
	admin.must("POST", "/api/v1/admin/groups", obj{"displayName": "Customers", "orgRole": "customer"}, 400, nil)

	// Tokens and MCP: a token made before someone became a customer stops reaching anything.
	dan := e.login("dan@example.com", "Dan Developer")
	var tok obj
	dan.must("POST", "/api/v1/me/tokens", obj{"name": "cli", "scopes": []string{"read", "write"}, "expiresInDays": 30}, 201, &tok)
	admin.must("PATCH", "/api/v1/admin/users/"+dan.id(), obj{"orgRole": "customer"}, 200, nil)
	danTok := e.withToken(str(tok, "token"))
	eq(t, danTok.do("GET", "/api/v1/issues/"+key, nil).Status, 403, "token: agent API")
	eq(t, danTok.do("GET", "/api/v1/search?jql=project+%3D+HELP", nil).Status, 403, "token: search")
	mcp := danTok.do("POST", "/mcp", []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_issue","arguments":{"key":"`+key+`"}}}`),
		"Content-Type", "application/json", "Accept", "application/json, text/event-stream")
	eq(t, mcp.Status, 403, "MCP refuses customers")
	if strings.Contains(string(mcp.Body), "SECRET") {
		t.Fatal("MCP leaked a note")
	}

	// Customers get no in-app notifications (which could quote internal notes); their emails
	// carry public replies only.
	var n int
	if err := e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM notifications WHERE user_id = $1`, uuid.MustParse(carol.id())).Scan(&n); err != nil {
		t.Fatal(err)
	}
	eq(t, n, 0, "no in-app notifications for customers")
	mail := e.mailText("carol@customer.test")
	if strings.Contains(mail, "SECRET") || !strings.Contains(mail, "Looking into it") {
		t.Fatalf("customer email:\n%s", mail)
	}

	// Agents see everything, labelled; MCP get_issue says which comment is which.
	agentTok := agent.newToken("assistant")
	res := e.withToken(agentTok).do("POST", "/mcp", []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_issue","arguments":{"key":"`+key+`"}}}`),
		"Content-Type", "application/json", "Accept", "application/json, text/event-stream")
	out := string(res.Body)
	if !strings.Contains(out, "[internal note]: SECRET-NOTE") || !strings.Contains(out, "[reply to customer]: Looking into it") || !strings.Contains(out, "Service request: Get IT help") {
		t.Fatalf("MCP get_issue:\n%s", out)
	}

	// Outbound webhooks say an internal note is internal, so receivers can keep it in-house.
	var hook obj
	admin.must("POST", "/api/v1/webhooks", obj{"projectKey": "HELP", "name": "audit", "url": "http://127.0.0.1:9/hook", "events": []string{"comment.created"}}, 201, &hook)
	agent.must("POST", "/api/v1/issues/"+key+"/comments", obj{"body": "SECRET-2"}, 201, nil)
	e.DrainJobs()
	var payload []byte
	if err := e.Pool.QueryRow(context.Background(), `SELECT payload FROM webhook_deliveries ORDER BY created_at DESC LIMIT 1`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var p struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(payload, &p)
	eq(t, p.Data["visibility"], any("internal"), "webhook payload marks internal notes")

	// Members of the organisation who raise requests in a private desk follow them like
	// customers: no notifications that could quote internal notes.
	admin.must("PATCH", "/api/v1/projects/HELP/service-desk", obj{"portalAccess": "organisation"}, 200, nil)
	mo := e.login("mo@example.com", "Mo Member")
	mr := mo.raise("HELP", str(rt, "id"), obj{"summary": "Mo's request"})
	eq(t, mo.do("GET", "/api/v1/issues/"+str(mr, "key"), nil).Status, 404, "the project stays private to Mo")
	agent.must("POST", "/api/v1/issues/"+str(mr, "key")+"/comments", obj{"body": "SECRET-3 about Mo"}, 201, nil)
	e.DrainJobs()
	var notes struct {
		Items []obj `json:"items"`
	}
	mo.get("/api/v1/notifications", &notes)
	for _, x := range notes.Items {
		if strings.Contains(fmt.Sprint(x), "SECRET") {
			t.Fatalf("member reporter notified of an internal note: %v", x)
		}
	}
	if strings.Contains(e.mailText("mo@example.com"), "SECRET") {
		t.Fatal("member reporter emailed an internal note")
	}
}

// token creates a personal access token for the client's user.
func (c *Client) newToken(name string) string {
	c.e.t.Helper()
	var tok obj
	c.must("POST", "/api/v1/me/tokens", obj{"name": name, "scopes": []string{"read", "write"}, "expiresInDays": 30}, 201, &tok)
	return str(tok, "token")
}

func TestParticipantsAndSelfSignup(t *testing.T) {
	e := newEnv(t)
	admin, agent := serviceDesk(t, e, "HELP")
	carol := admin.addCustomer("HELP", "carol@customer.test", "Carol Customer")
	fay := admin.addCustomer("HELP", "fay@customer.test", "Fay Friend")
	rt := admin.requestType("HELP", "Get IT help")
	r := carol.raise("HELP", str(rt, "id"), obj{"summary": "Shared printer"})
	key := str(r, "key")

	// Sharing: only with people who may use the portal.
	agent.must("POST", "/api/v1/issues/"+key+"/participants", obj{"email": "stranger@nowhere.test"}, 400, nil)
	var sr obj
	agent.must("POST", "/api/v1/issues/"+key+"/participants", obj{"email": "fay@customer.test"}, 200, &sr)
	eq(t, len(sr["participants"].([]any)), 1, "shared")
	var list []obj
	fay.get("/api/v1/portal/requests", &list)
	sameStrings(t, keys(list), []string{key}, "Fay follows it")
	eq(t, str(list[0], "role"), "participant", "as a participant")
	if !strings.Contains(e.mailText("fay@customer.test"), key+" was shared with you") {
		t.Fatal("shared email missing")
	}
	fay.must("POST", "/api/v1/portal/requests/"+key+"/comments", obj{"body": "Me too"}, 201, nil)
	agent.must("DELETE", "/api/v1/issues/"+key+"/participants/"+fay.id(), nil, 204, nil)
	eq(t, fay.do("GET", "/api/v1/portal/requests/"+key, nil).Status, 404, "no longer shared")

	// Customers on the list can be removed; their requests stay theirs.
	agent.must("DELETE", "/api/v1/projects/HELP/customers/"+carol.id(), nil, 204, nil)
	eq(t, carol.do("GET", "/api/v1/portal/projects/HELP", nil).Status, 404, "cannot raise any more")
	carol.get("/api/v1/portal/requests/"+key, nil)

	// Self sign-up is off by default; when on, it creates a customer confirmed by email.
	anon := e.anon()
	eq(t, anon.do("GET", "/api/v1/portal/signup/HELP", nil).Status, 404, "off")
	eq(t, anon.do("POST", "/api/v1/portal/signup/HELP", obj{"email": "new@customer.test", "displayName": "New Person"}).Status, 404, "off")
	admin.must("PATCH", "/api/v1/projects/HELP/service-desk", obj{"selfSignup": true}, 200, nil)
	var info obj
	anon.get("/api/v1/portal/signup/HELP", &info)
	eq(t, str(info, "name"), "HELP Help", "portal name")
	anon.must("POST", "/api/v1/portal/signup/HELP", obj{"email": "New@Customer.test", "displayName": "New Person"}, 202, nil)
	existing := anon.do("POST", "/api/v1/portal/signup/HELP", obj{"email": "fay@customer.test", "displayName": "Whoever"})
	eq(t, existing.Status, 202, "same answer for an existing address")
	msgs := e.mailTo("new@customer.test")
	eq(t, len(msgs), 1, "confirmation email")
	tok := linkToken(t, msgs[0], "invite")
	c := e.anon()
	c.must("POST", "/api/v1/auth/local/link/redeem", obj{"token": tok, "password": "Granite-Willow-Signal-84"}, 200, nil)
	wantStatus(t, c.passwordLogin("new@customer.test", "Granite-Willow-Signal-84"), 200, "signed_in")
	var desks []obj
	c.get("/api/v1/portal/projects", &desks)
	eq(t, len(desks), 1, "the new customer may use the desk")
	if got := e.mailText("fay@customer.test"); !strings.Contains(got, "You already have an account") {
		t.Fatalf("existing account email:\n%s", got)
	}
	var me obj
	c.get("/api/v1/me", &me)
	eq(t, str(me, "orgRole"), "customer", "a customer account")
	_ = time.Now
}

func TestApprovalsAfterTheRequestMovesOn(t *testing.T) {
	e := newEnv(t)
	admin, agent := serviceDesk(t, e, "HELP")
	carol := admin.addCustomer("HELP", "carol@customer.test", "Carol Customer")
	dave := admin.addCustomer("HELP", "dave@customer.test", "Dave Manager")
	rt := admin.requestType("HELP", "Request new hardware")
	admin.must("PUT", "/api/v1/request-types/"+str(rt, "id"), obj{
		"name": "Request new hardware", "issueTypeKey": "service_request",
		"fields":   []obj{{"field": "summary", "label": "What do you need?", "required": true}},
		"approval": obj{"mode": "any", "approverIds": []string{dave.id(), carol.id()}, "approvedStatus": "Waiting for support", "declinedStatus": "Declined"},
	}, 200, nil)

	// Carol is a named approver, but nobody approves their own request.
	key := str(carol.raise("HELP", str(rt, "id"), obj{"summary": "A laptop"}), "key")
	approvers := agent.serviceRequest(key)["approvals"].([]any)[0].(map[string]any)["approvers"].([]any)
	eq(t, len(approvers), 1, "Dave only")
	eq(t, str(approvers[0].(map[string]any), "person", "displayName"), "Dave Manager", "approver")
	carol.must("POST", "/api/v1/portal/requests/"+key+"/approval", obj{"decision": "approve"}, 403, nil)

	// A rule declines it while Dave has not decided (say, after a week).
	id := uuid.MustParse(str(agent.issue(key), "id"))
	moved, err := e.App.Tracker.TransitionToStatus(context.Background(), authz.AutomationActor(uuid.New(), 0), id, "Declined", "")
	if err != nil || !moved {
		t.Fatalf("rule did not decline: moved=%v err=%v", moved, err)
	}
	var pending []obj
	dave.get("/api/v1/portal/approvals", &pending)
	eq(t, len(pending), 0, "no longer waiting for Dave")
	// Dave's late approval is refused, and does not reopen the declined request.
	eq(t, dave.do("POST", "/api/v1/portal/requests/"+key+"/approval", obj{"decision": "approve"}).Status, 409, "late decision")
	eq(t, str(agent.issue(key), "status", "name"), "Declined", "still declined")
}

func TestTransitionRepliesReachTheCustomer(t *testing.T) {
	e := newEnv(t)
	admin, agent := serviceDesk(t, e, "HELP")
	carol := admin.addCustomer("HELP", "carol@customer.test", "Carol Customer")
	key := str(carol.raise("HELP", str(admin.requestType("HELP", "Get IT help"), "id"), obj{"summary": "Printer jam"}), "key")
	agent.transitionTo(key, "resolve", obj{"fields": obj{"resolution": "Done"}, "comment": "Cleared the jam in tray 2", "commentVisibility": "public"})
	mail := e.mailText("carol@customer.test")
	if !strings.Contains(mail, key+" is resolved") || !strings.Contains(mail, "Cleared the jam in tray 2") {
		t.Fatalf("the resolution email should carry the reply:\n%s", mail)
	}
	// Moving the request without a special email still sends the reply on its own.
	agent.transitionTo(key, "reopen", nil)
	agent.transitionTo(key, "start-progress", obj{"comment": "Ordering a new roller", "commentVisibility": "public"})
	if mail := e.mailText("carol@customer.test"); !strings.Contains(mail, "Ordering a new roller") {
		t.Fatalf("reply on a plain move not emailed:\n%s", mail)
	}
}

func TestSelfSignupLeavesStaffAndSignInDomainsAlone(t *testing.T) {
	e := oidcEnv(t, newFakeIdP(t)) // signs in example.com through the identity provider
	admin, agent := serviceDesk(t, e, "HELP")
	admin.must("PATCH", "/api/v1/projects/HELP/service-desk", obj{"selfSignup": true}, 200, nil)
	anon := e.anon()
	// Someone at the organisation's own domain must not claim the address as a customer first.
	eq(t, anon.do("POST", "/api/v1/portal/signup/HELP", obj{"email": "newhire@example.com", "displayName": "New Hire"}).Status, 400, "sign-in domain")
	eq(t, len(e.mailTo("newhire@example.com")), 0, "no account was made")
	// A staff account elsewhere is told it has an account, and is not listed as a customer.
	e.login("sam@partner.test", "Sam Staff")
	eq(t, anon.do("POST", "/api/v1/portal/signup/HELP", obj{"email": "sam@partner.test", "displayName": "Sam"}).Status, 202, "same answer")
	var customers []obj
	agent.get("/api/v1/projects/HELP/customers", &customers)
	for _, c := range customers {
		if strings.Contains(fmt.Sprint(c), "sam@partner.test") {
			t.Fatal("a staff account was added to the desk's customers")
		}
	}
}
