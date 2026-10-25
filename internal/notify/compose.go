package notify

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/workflowproj/workflow/internal/mail"
)

// actorName is who caused a notification; changes without a person are automation's.
func actorName(n pending) string {
	if n.ActorName != "" {
		return n.ActorName
	}
	return "Automation"
}

// event describes one notification for the templates.
func event(n pending, loc *time.Location, layout string) Event {
	e := Event{Actor: actorName(n), When: n.CreatedAt.In(loc).Format(layout)}
	switch n.Kind {
	case "assigned":
		e.What = "Assigned this issue to you"
	case "commented":
		e.What = "Commented"
		e.Excerpt = Excerpt(n.Body, excerptLength)
	default:
		e.What = n.Body
		if e.What == "" {
			e.What = n.Title
		}
	}
	return e
}

// headline says what happened, e.g. "Ana Lima commented on PLAT-12" or "3 updates on PLAT-12".
func headline(key string, events []pending) string {
	if len(events) > 1 {
		return fmt.Sprintf("%d updates on %s", len(events), key)
	}
	n := events[0]
	who := actorName(n)
	switch n.Kind {
	case "assigned":
		if n.ActorName == "" {
			return key + " was assigned to you"
		}
		return who + " assigned " + key + " to you"
	case "commented":
		return who + " commented on " + key
	case "transitioned":
		return who + " changed the status of " + key
	case "automation":
		return "Automation update on " + key
	}
	return n.Title
}

// preheader is the preview text: who did what.
func preheader(events []pending) string {
	var parts []string
	for _, n := range events {
		e := event(n, time.UTC, "")
		line := e.Actor + ": " + e.What
		if e.Excerpt != "" {
			line += " — " + Excerpt(strings.ReplaceAll(e.Excerpt, "\n", " "), 120)
		}
		parts = append(parts, line)
		if len(parts) == 3 {
			break
		}
	}
	return strings.Join(parts, " · ")
}

// kindsOf lists the kinds among notifications, in the order of Kinds.
func kindsOf(events []pending) []string {
	var out []string
	for _, k := range Kinds {
		if slices.ContainsFunc(events, func(n pending) bool { return n.Kind == k.Key }) {
			out = append(out, k.Key)
		}
	}
	return out
}

// stopLabel names an unsubscribe link: "Stop emails about comments and status changes".
func stopLabel(kinds []string) string {
	var nouns []string
	for _, k := range kinds {
		if kind, ok := KindByKey(k); ok {
			nouns = append(nouns, kind.Noun)
		}
	}
	switch len(nouns) {
	case 0:
		return "Unsubscribe"
	case 1:
		return "Stop emails about " + nouns[0]
	}
	return "Stop emails about " + strings.Join(nouns[:len(nouns)-1], ", ") + " and " + nouns[len(nouns)-1]
}

// reason says why the person got an email about an issue.
func reason(r recipient, g *issueGroup) string {
	switch {
	case g.issue.AssigneeID != nil && *g.issue.AssigneeID == r.user.ID:
		return "You're receiving this because you're assigned to " + g.key + "."
	case g.issue.ReporterID != nil && *g.issue.ReporterID == r.user.ID:
		return "You're receiving this because you reported " + g.key + "."
	case g.watching:
		return "You're receiving this because you're watching " + g.key + "."
	}
	return "You're receiving this because an automation rule in " + g.issue.ProjectName + " notifies you."
}

func (s *Service) block(g *issueGroup, events []Event, more int, open bool) IssueBlock {
	return IssueBlock{
		Key: g.key, Summary: g.issue.Summary, URL: s.issueURL(g.key), Project: g.issue.ProjectName,
		Status: g.issue.StatusName, StatusCategory: g.issue.StatusCategory, Events: events, MoreEvents: more, Open: open,
	}
}

// immediateEmail is one issue's notifications from the last few minutes.
func (s *Service) immediateEmail(r recipient, g *issueGroup) (mail.Message, error) {
	var events []Event
	for _, n := range g.events {
		events = append(events, event(n, r.loc, "15:04"))
	}
	kinds := kindsOf(g.events)
	e := Email{
		Brand:     s.Brand(),
		Subject:   fmt.Sprintf("[%s] %s", g.key, g.issue.Summary),
		Preheader: preheader(g.events),
		Headline:  headline(g.key, g.events),
		Issues:    []IssueBlock{s.block(g, events, 0, false)},
		Button:    &Link{Label: "View " + g.key, URL: s.issueURL(g.key)},
		Reason:    reason(r, g),
		ManageURL: s.ManageURL(),
	}
	unsub := s.UnsubscribeURL(r.user.ID, "kinds:"+strings.Join(kinds, ","))
	e.Unsub = &Link{Label: stopLabel(kinds), URL: unsub}
	return s.message(r.user.Email, e, unsub, s.threadID(g.issue.ID))
}

// digestEmail summarises everything since the last digest, most recently active issues first.
func (s *Service) digestEmail(r recipient, groups []*issueGroup) (mail.Message, error) {
	total := 0
	for _, g := range groups {
		total += len(g.events)
	}
	slices.SortStableFunc(groups, func(a, b *issueGroup) int {
		return b.events[len(b.events)-1].CreatedAt.Compare(a.events[len(a.events)-1].CreatedAt)
	})
	summary := fmt.Sprintf("%d update%s on %d issue%s", total, plural(total), len(groups), plural(len(groups)))
	e := Email{
		Brand:     s.Brand(),
		Subject:   "Daily digest: " + summary,
		Headline:  "Your daily digest: " + summary,
		Button:    &Link{Label: "Open notifications", URL: s.Cfg.PublicURL + "/notifications"},
		Reason:    "You're receiving this daily digest because some of your email notifications are set to “Daily digest”.",
		ManageURL: s.ManageURL(),
		TimeZone:  strings.ReplaceAll(r.settings.TimeZone, "_", " "),
	}
	if last := r.settings.LastDigestAt; last != nil {
		e.Intro = []string{"What happened since your last digest on " + last.In(r.loc).Format("Mon 2 Jan at 15:04") + "."}
	} else {
		e.Intro = []string{"What happened on issues you're involved in."}
	}
	var names []string
	for i, g := range groups {
		if i == digestMaxIssues {
			e.MoreIssues = len(groups) - digestMaxIssues
			e.MoreURL = s.Cfg.PublicURL + "/notifications"
			break
		}
		evs := g.events
		more := 0
		if len(evs) > digestMaxEvents {
			more = len(evs) - digestMaxEvents
			evs = evs[len(evs)-digestMaxEvents:]
		}
		var events []Event
		for _, n := range evs {
			events = append(events, event(n, r.loc, "Mon 15:04"))
		}
		e.Issues = append(e.Issues, s.block(g, events, more, true))
		if len(names) < 3 {
			names = append(names, g.key+" "+g.issue.Summary)
		}
	}
	e.Preheader = strings.Join(names, " · ")
	unsub := s.UnsubscribeURL(r.user.ID, "digest")
	e.Unsub = &Link{Label: "Stop the daily digest", URL: unsub}
	return s.message(r.user.Email, e, unsub, "")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
