// Package jobs declares the background job arguments processed by the worker (River).
// Jobs are enqueued with InsertTx inside the same transaction as the change that caused them,
// which gives the outbox pattern without a separate broker.
package jobs

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// Enqueuer is satisfied by *river.Client[pgx.Tx].
type Enqueuer interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

const (
	QueueDefault = river.QueueDefault
	// QueueSCM isolates webhook processing so bursts do not delay other work.
	QueueSCM = "scm"
	// QueueOutbound holds calls to third parties (outbound webhooks, write-back) that may be slow.
	QueueOutbound = "outbound"
)

// DomainEvent fans a tracker change out to automation rules, outbound webhooks and notifications.
type DomainEvent struct {
	Event     string          `json:"event"`
	ProjectID uuid.UUID       `json:"projectId"`
	IssueID   *uuid.UUID      `json:"issueId,omitempty"`
	ActorID   *uuid.UUID      `json:"actorId,omitempty"`
	ActorType string          `json:"actorType"`
	Depth     int             `json:"depth"`
	RuleID    *uuid.UUID      `json:"ruleId,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

func (DomainEvent) Kind() string { return "domain_event" }

// SendEmail delivers one email (see the mail package, which queues these).
type SendEmail struct {
	To      []string          `json:"to"`
	Subject string            `json:"subject"`
	Text    string            `json:"text"`
	HTML    string            `json:"html,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (SendEmail) Kind() string { return "email_send" }

func (SendEmail) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueOutbound, MaxAttempts: 8}
}

// EmailNotify emails one person what happened on one issue since their last email about it.
// It is scheduled a short window ahead and unique per person, issue and window, so a burst of
// changes becomes one email (see notify.Schedule).
type EmailNotify struct {
	UserID  uuid.UUID `json:"userId"`
	IssueID uuid.UUID `json:"issueId"`
}

func (EmailNotify) Kind() string { return "email_notify" }

// EmailDigests queues a digest for everyone due one (periodic).
type EmailDigests struct{}

func (EmailDigests) Kind() string { return "email_digests" }

// EmailDigest sends one person's daily digest of notifications.
type EmailDigest struct {
	UserID uuid.UUID `json:"userId"`
}

func (EmailDigest) Kind() string { return "email_digest" }

func (EmailDigest) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateScheduled, rivertype.JobStateRunning, rivertype.JobStateRetryable}}}
}

// ProcessWebhook processes a stored inbound SCM webhook event.
type ProcessWebhook struct {
	EventID uuid.UUID `json:"eventId"`
}

func (ProcessWebhook) Kind() string { return "scm_webhook" }

func (ProcessWebhook) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueSCM, MaxAttempts: 10}
}

// SCMWriteBack comments on a PR/MR that links an issue, or sets a check run / commit status.
type SCMWriteBack struct {
	DevLinkID uuid.UUID `json:"devLinkId"`
	IssueID   uuid.UUID `json:"issueId"`
}

func (SCMWriteBack) Kind() string { return "scm_writeback" }

func (SCMWriteBack) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueOutbound, MaxAttempts: 8}
}

// SCMReconcile lists repositories and recent activity from provider APIs to fill gaps from missed deliveries.
type SCMReconcile struct {
	ConnectionID *uuid.UUID `json:"connectionId,omitempty"`
}

func (SCMReconcile) Kind() string { return "scm_reconcile" }

func (SCMReconcile) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueOutbound, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateScheduled, rivertype.JobStateRunning}}}
}

// SCMEnsureHook creates a project webhook through the GitLab API (Free tier has no group hooks).
type SCMEnsureHook struct {
	RepositoryID uuid.UUID `json:"repositoryId"`
}

func (SCMEnsureHook) Kind() string { return "scm_ensure_hook" }

func (SCMEnsureHook) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueOutbound, MaxAttempts: 5}
}

// WorkflowMigrate moves issues onto a workflow's current version, mapping removed statuses.
type WorkflowMigrate struct {
	WorkflowID uuid.UUID `json:"workflowId"`
	// Mapping of old status id -> new status id.
	Mapping map[string]string `json:"mapping"`
}

func (WorkflowMigrate) Kind() string { return "workflow_migrate" }

func (WorkflowMigrate) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 10} }

// PostFunction runs a side-effecting workflow post-function (notify, webhook).
type PostFunction struct {
	IssueID    uuid.UUID  `json:"issueId"`
	ActorID    *uuid.UUID `json:"actorId,omitempty"`
	Transition string     `json:"transition"`
	FromStatus string     `json:"fromStatus"`
	ToStatus   string     `json:"toStatus"`
	Notify     string     `json:"notify,omitempty"`
	Webhook    string     `json:"webhook,omitempty"`
}

func (PostFunction) Kind() string { return "post_function" }

func (p PostFunction) InsertOpts() river.InsertOpts {
	if p.Webhook != "" {
		return river.InsertOpts{Queue: QueueOutbound, MaxAttempts: 8}
	}
	return river.InsertOpts{}
}

// DeliverWebhook sends one outbound webhook delivery, retried with backoff.
type DeliverWebhook struct {
	DeliveryID uuid.UUID `json:"deliveryId"`
}

func (DeliverWebhook) Kind() string { return "webhook_deliver" }

func (DeliverWebhook) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueOutbound, MaxAttempts: 8}
}

// DeliverChat posts one Slack or Mattermost message, retried with backoff.
type DeliverChat struct {
	DeliveryID uuid.UUID `json:"deliveryId"`
}

func (DeliverChat) Kind() string { return "chat_deliver" }

func (DeliverChat) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueOutbound, MaxAttempts: 6}
}

// DirectorySync reads the Google Workspace directory and reconciles users and groups.
type DirectorySync struct {
	Trigger string `json:"trigger"`
}

func (DirectorySync) Kind() string { return "directory_sync" }

func (DirectorySync) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateScheduled, rivertype.JobStateRunning}}}
}

// AutomationSchedule runs scheduled automation rules that are due.
type AutomationSchedule struct{}

func (AutomationSchedule) Kind() string { return "automation_schedule" }

// Maintenance purges expired sessions, anonymises deleted users past retention and prunes old webhook events.
type Maintenance struct{}

func (Maintenance) Kind() string { return "maintenance" }

// JiraImport runs a scan or an import of a Jira site (see the jiraimport package) in
// checkpointed steps; when a job has worked for its time budget it enqueues the next Seq.
type JiraImport struct {
	ImportID uuid.UUID `json:"importId"`
	RunID    uuid.UUID `json:"runId"`
	Mode     string    `json:"mode"`
	ActorID  uuid.UUID `json:"actorId"`
	Seq      int       `json:"seq"`
}

func (JiraImport) Kind() string { return "jira_import" }

func (JiraImport) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueOutbound, MaxAttempts: 5, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}
