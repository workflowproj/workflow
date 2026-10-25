-- ---------------------------------------------------------------------------
-- Service desks and their customers

-- name: CreateServiceDesk :one
INSERT INTO service_desks (project_id, portal_name, portal_description) VALUES (@project_id, @portal_name, @portal_description)
ON CONFLICT (project_id) DO UPDATE SET updated_at = now()
RETURNING *;

-- name: GetServiceDesk :one
SELECT * FROM service_desks WHERE project_id = $1;

-- name: UpdateServiceDesk :one
UPDATE service_desks SET portal_name = @portal_name, portal_description = @portal_description,
    portal_access = @portal_access, self_signup = @self_signup, updated_at = now()
WHERE project_id = @project_id
RETURNING *;

-- name: ListServiceDesks :many
SELECT sqlc.embed(service_desks), sqlc.embed(projects)
FROM service_desks JOIN projects ON projects.id = service_desks.project_id
WHERE projects.archived_at IS NULL
ORDER BY lower(service_desks.portal_name);

-- name: AddServiceCustomer :exec
INSERT INTO service_customers (project_id, user_id, added_by) VALUES (@project_id, @user_id, @added_by)
ON CONFLICT DO NOTHING;

-- name: RemoveServiceCustomer :execrows
DELETE FROM service_customers WHERE project_id = $1 AND user_id = $2;

-- name: IsServiceCustomer :one
SELECT EXISTS (SELECT 1 FROM service_customers WHERE project_id = $1 AND user_id = $2);

-- name: ListServiceCustomers :many
SELECT sqlc.embed(users), service_customers.created_at AS added_at
FROM service_customers JOIN users ON users.id = service_customers.user_id
WHERE service_customers.project_id = @project_id AND users.deleted_at IS NULL
  AND (@q::text = '' OR users.display_name ILIKE '%' || @q || '%' OR users.email ILIKE '%' || @q || '%')
ORDER BY lower(users.display_name)
LIMIT @lim;

-- name: CountServiceCustomers :one
SELECT count(*) FROM service_customers WHERE project_id = $1;

-- ---------------------------------------------------------------------------
-- Request types

-- name: CreateRequestType :one
INSERT INTO request_types (project_id, name, description, icon, issue_type_id, portal_group, position, form, approval)
VALUES (@project_id, @name, @description, @icon, @issue_type_id, @portal_group, @position, @form, @approval)
RETURNING *;

-- name: UpdateRequestType :one
UPDATE request_types SET name = @name, description = @description, icon = @icon, issue_type_id = @issue_type_id,
    portal_group = @portal_group, position = @position, form = @form, approval = @approval, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: GetRequestType :one
SELECT * FROM request_types WHERE id = $1;

-- name: ListRequestTypes :many
SELECT * FROM request_types WHERE project_id = $1 ORDER BY position, lower(name);

-- name: DeleteRequestType :exec
DELETE FROM request_types WHERE id = $1;

-- name: CountRequestsOfType :one
SELECT count(*) FROM service_requests WHERE request_type_id = $1;

-- name: CountRequestTypes :one
SELECT count(*) FROM request_types WHERE project_id = $1;

-- ---------------------------------------------------------------------------
-- Requests, participants

-- name: UpsertServiceRequest :one
INSERT INTO service_requests (issue_id, project_id, request_type_id, channel)
VALUES (@issue_id, @project_id, @request_type_id, @channel)
ON CONFLICT (issue_id) DO UPDATE SET request_type_id = excluded.request_type_id
RETURNING *;

-- name: GetServiceRequest :one
SELECT * FROM service_requests WHERE issue_id = $1;

-- name: ServiceRequestsFor :many
SELECT * FROM service_requests WHERE issue_id = ANY(@ids::uuid[]);

-- name: PortalRequests :many
-- Requests a person follows in the portal: they reported them or they are shared with them.
-- Issues without a request type are never shown there.
SELECT sqlc.embed(i), sr.request_type_id
FROM issues i
JOIN service_requests sr ON sr.issue_id = i.id
JOIN statuses s ON s.id = i.status_id
WHERE sr.request_type_id IS NOT NULL
  AND (i.reporter_id = @user_id OR EXISTS (SELECT 1 FROM request_participants rp WHERE rp.issue_id = i.id AND rp.user_id = @user_id))
  AND (sqlc.narg(project_id)::uuid IS NULL OR i.project_id = sqlc.narg(project_id))
  AND (sqlc.narg(done)::boolean IS NULL OR (s.category = 'done') = sqlc.narg(done))
ORDER BY i.updated_at DESC
LIMIT @lim;

-- name: PendingApprovalRequests :many
-- Requests waiting for a person's decision.
SELECT DISTINCT ON (i.updated_at, i.id) sqlc.embed(i), sr.request_type_id
FROM approval_approvers aa
JOIN request_approvals ra ON ra.id = aa.approval_id AND ra.status = 'pending'
JOIN issues i ON i.id = ra.issue_id
JOIN service_requests sr ON sr.issue_id = i.id
WHERE aa.user_id = @user_id AND aa.decision IS NULL AND sr.request_type_id IS NOT NULL
ORDER BY i.updated_at DESC, i.id
LIMIT @lim;

-- name: AddParticipant :exec
INSERT INTO request_participants (issue_id, user_id, added_by) VALUES (@issue_id, @user_id, @added_by) ON CONFLICT DO NOTHING;

-- name: RemoveParticipant :execrows
DELETE FROM request_participants WHERE issue_id = $1 AND user_id = $2;

-- name: ListParticipants :many
SELECT u.* FROM request_participants rp JOIN users u ON u.id = rp.user_id WHERE rp.issue_id = $1 ORDER BY lower(u.display_name);

-- name: IsParticipant :one
SELECT EXISTS (SELECT 1 FROM request_participants WHERE issue_id = $1 AND user_id = $2);

-- name: ListPublicComments :many
SELECT * FROM comments WHERE issue_id = $1 AND NOT internal ORDER BY created_at;

-- name: ListPublicAttachments :many
SELECT * FROM attachments WHERE issue_id = $1 AND public ORDER BY created_at;

-- name: FirstPublicReply :one
-- When an agent first replied to the customer: the first comment they can see that was not
-- written in the portal.
SELECT created_at FROM comments WHERE issue_id = $1 AND NOT internal AND source <> 'portal' ORDER BY created_at LIMIT 1;

-- name: StatusChanges :many
SELECT created_at, from_ref, to_ref FROM issue_history WHERE issue_id = $1 AND field = 'status' ORDER BY created_at, id;

-- ---------------------------------------------------------------------------
-- Queues

-- name: CreateQueue :one
INSERT INTO service_queues (project_id, name, jql, position) VALUES (@project_id, @name, @jql, @position) RETURNING *;

-- name: UpdateQueue :one
UPDATE service_queues SET name = @name, jql = @jql, position = @position, updated_at = now() WHERE id = @id RETURNING *;

-- name: GetQueue :one
SELECT * FROM service_queues WHERE id = $1;

-- name: ListQueues :many
SELECT * FROM service_queues WHERE project_id = $1 ORDER BY position, created_at;

-- name: DeleteQueue :exec
DELETE FROM service_queues WHERE id = $1;

-- ---------------------------------------------------------------------------
-- SLAs: calendars, metrics and goals

-- name: UpsertSLACalendar :one
INSERT INTO sla_calendars (project_id, name, time_zone, hours, holidays)
VALUES (@project_id, @name, @time_zone, @hours, @holidays)
ON CONFLICT (project_id, lower(name)) DO UPDATE SET name = excluded.name, time_zone = excluded.time_zone,
    hours = excluded.hours, holidays = excluded.holidays, updated_at = now()
RETURNING *;

-- name: ListSLACalendars :many
SELECT * FROM sla_calendars WHERE project_id = $1 ORDER BY lower(name);

-- name: AllSLACalendars :many
SELECT * FROM sla_calendars;

-- name: GetSLACalendar :one
SELECT * FROM sla_calendars WHERE id = $1;

-- name: DeleteSLACalendarsExcept :exec
DELETE FROM sla_calendars WHERE project_id = @project_id AND NOT (id = ANY(@keep::uuid[]));

-- name: UpsertSLAMetric :exec
INSERT INTO sla_metrics (project_id, metric, name, pause_status_ids) VALUES (@project_id, @metric, @name, @pause_status_ids)
ON CONFLICT (project_id, metric) DO UPDATE SET name = excluded.name, pause_status_ids = excluded.pause_status_ids;

-- name: DeleteSLAMetrics :exec
-- Their goals go with them.
DELETE FROM sla_metrics WHERE project_id = $1;

-- name: ListSLAMetrics :many
SELECT * FROM sla_metrics WHERE project_id = $1 ORDER BY metric DESC;

-- name: DeleteSLAGoals :exec
DELETE FROM sla_goals WHERE project_id = $1;

-- name: CreateSLAGoal :one
INSERT INTO sla_goals (project_id, metric, position, priorities, request_type_ids, jql, target_minutes, calendar_id)
VALUES (@project_id, @metric, @position, @priorities, @request_type_ids, @jql, @target_minutes, @calendar_id)
RETURNING *;

-- name: ListSLAGoals :many
SELECT * FROM sla_goals WHERE project_id = $1 ORDER BY metric DESC, position;

-- ---------------------------------------------------------------------------
-- SLA clocks of requests

-- name: UpsertIssueSLA :exec
INSERT INTO issue_slas (issue_id, metric, project_id, goal_id, calendar_id, target_seconds, state, elapsed_seconds,
                        cycle_started_at, stopped_at, breach_at, breached, breached_at, computed_at)
VALUES (@issue_id, @metric, @project_id, @goal_id, @calendar_id, @target_seconds, @state, @elapsed_seconds,
        @cycle_started_at, @stopped_at, @breach_at, @breached, @breached_at, @computed_at)
ON CONFLICT (issue_id, metric) DO UPDATE SET
    goal_id = excluded.goal_id, calendar_id = excluded.calendar_id, target_seconds = excluded.target_seconds,
    state = excluded.state, elapsed_seconds = excluded.elapsed_seconds, cycle_started_at = excluded.cycle_started_at,
    stopped_at = excluded.stopped_at, breach_at = excluded.breach_at, breached = excluded.breached,
    breached_at = excluded.breached_at, computed_at = excluded.computed_at,
    -- A new cycle, or a clock no longer breached (a changed goal), may breach again.
    breach_notified_at = CASE WHEN excluded.breached_at IS NULL AND NOT excluded.breached THEN NULL
                              WHEN excluded.cycle_started_at <> issue_slas.cycle_started_at THEN NULL
                              ELSE issue_slas.breach_notified_at END;

-- name: DeleteIssueSLA :exec
DELETE FROM issue_slas WHERE issue_id = $1 AND metric = $2;

-- name: IssueSLAsFor :many
SELECT * FROM issue_slas WHERE issue_id = ANY(@ids::uuid[]) ORDER BY metric;

-- name: DueSLABreaches :many
-- Running clocks past their target that nobody was told about yet.
SELECT * FROM issue_slas
WHERE state = 'running' AND breach_notified_at IS NULL AND breach_at < @now
ORDER BY breach_at
LIMIT @lim;

-- name: MarkSLABreachNotified :exec
UPDATE issue_slas SET breach_notified_at = now(), breached = true WHERE issue_id = $1 AND metric = $2;

-- name: OpenRequestIDs :many
-- Requests of a service project whose clocks may still change (recomputed after the SLA
-- configuration changes).
SELECT id FROM issues WHERE project_id = $1 AND resolved_at IS NULL ORDER BY id;

-- ---------------------------------------------------------------------------
-- Approvals

-- name: CreateApproval :one
INSERT INTO request_approvals (issue_id, mode, approved_status, declined_status)
VALUES (@issue_id, @mode, @approved_status, @declined_status)
RETURNING *;

-- name: AddApprover :exec
INSERT INTO approval_approvers (approval_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: ListApprovals :many
SELECT * FROM request_approvals WHERE issue_id = $1 ORDER BY created_at;

-- name: GetApprovalForUpdate :one
SELECT * FROM request_approvals WHERE id = $1 FOR UPDATE;

-- name: ListApprovers :many
SELECT aa.approval_id, aa.decision, aa.comment, aa.decided_at, sqlc.embed(users)
FROM approval_approvers aa JOIN users ON users.id = aa.user_id
WHERE aa.approval_id = ANY(@ids::uuid[])
ORDER BY lower(users.display_name);

-- name: SetApproverDecision :execrows
UPDATE approval_approvers SET decision = @decision, comment = @comment, decided_at = now()
WHERE approval_id = @approval_id AND user_id = @user_id AND decision IS NULL;

-- name: SetApprovalStatus :exec
UPDATE request_approvals SET status = @status, decided_at = now() WHERE id = @id;

-- name: IsApprover :one
SELECT EXISTS (
    SELECT 1 FROM approval_approvers aa JOIN request_approvals ra ON ra.id = aa.approval_id
    WHERE ra.issue_id = $1 AND aa.user_id = $2
);
