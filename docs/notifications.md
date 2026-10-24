# Notifications and email

People are notified in the app (the bell, and *Notifications*) and, depending on their
settings, by email: right away, in a daily digest, or not at all.

## What people are notified about

| Kind | Who | Email by default |
|---|---|---|
| **Assigned to me** | the new assignee, when an issue is created for them or assigned to them | immediately |
| **Comments** | the reporter, the assignee and everyone watching the issue | daily digest |
| **Status changes** | the reporter, the assignee and everyone watching the issue, plus whoever a workflow transition's `notify` post-function names | daily digest |
| **Automation** | whoever an automation rule's *Notify* action names | daily digest |

Nobody is notified about their own changes. Creating an issue, commenting on it or being
assigned to it makes you a watcher; anyone can watch or stop watching an issue from its page.

## Email notifications

Everyone chooses in **Profile → Email notifications**:

- per kind: **Immediately**, **Daily digest** or **Off**;
- when the digest arrives: an hour of the day and a time zone (UTC until changed; the page
  offers the browser's zone);
- **Pause all**, which stops every notification email until it is turned off again. Nothing
  piles up while paused: notifications from that time stay in the app only.

The same settings are available to API and MCP clients at
`GET`/`PATCH /api/v1/me/email-notifications`.

Emails only go to active people, never to service accounts, and only about issues in projects
the person can still browse: if someone loses access, or is deactivated, between a change and
the email, the email is not sent.

### Immediate emails

An immediate email waits a short window (`WFM_EMAIL_BATCH_WINDOW`, 2 minutes) and then sends
everything that happened to the issue for that person meanwhile, so an assignment, a comment
and a status change in quick succession arrive as one email ("3 updates on PLAT-12"). The
subject is the issue key and summary, and the emails about one issue carry the same
`References` header so mail clients thread them.

### The daily digest

Once a day at the chosen hour, people with anything waiting get one email summarising it,
grouped by issue (most recently active first, up to 25 issues and 8 updates per issue, with a
link to the rest). The worker checks for due digests every 5 minutes, so a digest can arrive a
few minutes after the hour; a digest is only sent when something happened.

### What an email contains

What happened and who did it, the issue key, summary, current status and project, a comment
excerpt (about 600 characters; inline Markdown — code, bold and links — is rendered, everything
else is shown as written), a button to the issue at `WFM_PUBLIC_URL/browse/KEY`, and a footer
saying why the email was sent, with **Manage email notifications** and an unsubscribe link.
Every email has a plain-text part and a simple HTML part with inline styles and no remote
images; clients that support it get a dark version.

### Unsubscribing

Every notification email has a one-click unsubscribe link, also offered to mail clients
through the `List-Unsubscribe` and `List-Unsubscribe-Post: List-Unsubscribe=One-Click` headers
([RFC 8058](https://www.rfc-editor.org/rfc/rfc8058)). The link of an immediate email turns off
the kinds of notification it contained; the digest's link turns off the digest (every kind set
to *Daily digest*). The page behind the link asks before changing anything (so link scanners
cannot unsubscribe anyone) and also offers to pause all notification emails; mail providers'
one-click requests (`POST`) apply at once.

The link is `WFM_HOOKS_PUBLIC_URL/hooks/email/unsubscribe?t=…`. It is served by the `hooks`
mode, the internet-facing one, because mail providers send one-click requests from the
internet; with a single public URL that is simply `WFM_PUBLIC_URL`. The token is signed with
`WFM_SECRET_KEY`, so it cannot be guessed or changed to unsubscribe someone else, and it does
not expire. Providers such as Gmail honour one-click unsubscribe only for DKIM-signed mail:
sign outgoing mail for the `WFM_SMTP_FROM` domain at your mail server or provider.

## Setting up email

Set `WFM_SMTP_HOST` and the other `WFM_SMTP_*` variables (see
[Configuration](configuration.md#email)) and restart. **Administration → Email** shows how mail
is sent, the sender address, where links point, and has **Send test email**, which sends to you
right away (not through the queue) so that a wrong host, port, TLS mode or password shows up
immediately as an error.

Without an SMTP server nothing is delivered: emails are logged and kept in memory. In
development (`WFM_ENV=development`) the kept emails are listed at
`GET /api/v1/dev/mailbox?to=address`.

## How it works

1. A change commits and its domain event is processed by the worker, which creates the in-app
   notifications. At that moment each recipient's settings decide how the notification is
   emailed (immediately, in the digest, or not), and that is stored with it.
2. For an immediate email, a job is scheduled one window ahead, unique per person, issue and
   window, so further changes join it. When it runs it sends everything still waiting for that
   person on that issue, then marks it emailed.
3. A periodic job (every 5 minutes) queues a digest job for each person whose digest time has
   passed since their oldest waiting notification; the digest job sends and records the time.
   The same periodic job requeues any immediate email that should long have gone (a safety net).
4. Emails are queued as `email_send` jobs in the same transaction that marks the notifications
   emailed, and sent with retries.

Before sending, each job checks again that the person is active, not paused, can still browse
the project and still wants that kind of email.
