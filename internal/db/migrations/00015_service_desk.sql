-- +goose Up
-- Service desk: service projects (the "service" template) with a customer portal, request types
-- and their forms, customers, replies to customers and internal notes, queues, SLAs and approvals.

-- Customers only use the portal: they raise requests in the service projects open to them and
-- see their own requests (and those shared with them), never the agent UI or its API.
ALTER TABLE users DROP CONSTRAINT users_org_role_check,
    ADD CONSTRAINT users_org_role_check CHECK (org_role IN ('admin', 'member', 'guest', 'customer'));

-- A comment on a service request is a reply to the customer or an internal note. Internal notes
-- never reach the portal, customer emails or anything a customer can call. Comments in software
-- projects are never internal.
ALTER TABLE comments ADD COLUMN internal boolean NOT NULL DEFAULT false;
-- Attachments the portal shows: those customers added there.
ALTER TABLE attachments ADD COLUMN public boolean NOT NULL DEFAULT false;

-- Agents may invite and remove the customers of a service project.
UPDATE roles SET permissions = array_append(permissions, 'customer.manage')
WHERE builtin AND key IN ('member', 'admin') AND NOT 'customer.manage' = ANY(permissions);

-- One row per service project: the portal and who may use it.
CREATE TABLE service_desks (
    project_id         uuid PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
    portal_name        text NOT NULL,
    portal_description text NOT NULL DEFAULT '',
    -- customers: people on the project's customer list; organisation: also every organisation
    -- member (not guests, not other projects' customers).
    portal_access      text NOT NULL DEFAULT 'customers' CHECK (portal_access IN ('customers', 'organisation')),
    -- Anyone may create a customer account from the portal, confirmed by email. Off by default.
    self_signup        boolean NOT NULL DEFAULT false,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

-- People who may raise requests in a service project.
CREATE TABLE service_customers (
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    added_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);
CREATE INDEX service_customers_user_idx ON service_customers (user_id);

-- What customers can ask for: the issue type it creates and the portal form.
CREATE TABLE request_types (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name          text NOT NULL,
    description   text NOT NULL DEFAULT '',
    icon          text NOT NULL DEFAULT 'help',
    issue_type_id uuid NOT NULL REFERENCES issue_types (id),
    -- Request types are grouped on the portal (in position order).
    portal_group  text NOT NULL DEFAULT '',
    position      integer NOT NULL DEFAULT 0,
    -- The ordered form: [{field, label, help, required}]; field is summary, description,
    -- priority, dueDate or a custom field key.
    form          jsonb NOT NULL DEFAULT '[]',
    -- Approval before work starts: {mode, userIds, groupId, approvedStatus, declinedStatus};
    -- NULL when none is needed.
    approval      jsonb,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX request_types_name_key ON request_types (project_id, lower(name));

-- A service request: an issue raised with a request type (through the portal, or by an agent).
-- The portal only shows issues that have one.
CREATE TABLE service_requests (
    issue_id        uuid PRIMARY KEY REFERENCES issues (id) ON DELETE CASCADE,
    project_id      uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    request_type_id uuid REFERENCES request_types (id) ON DELETE SET NULL,
    channel         text NOT NULL DEFAULT 'portal' CHECK (channel IN ('portal', 'agent')),
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX service_requests_type_idx ON service_requests (request_type_id);
CREATE INDEX service_requests_project_idx ON service_requests (project_id);

-- People a request is shared with: they follow it in the portal like its reporter.
CREATE TABLE request_participants (
    issue_id   uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    added_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, user_id)
);
CREATE INDEX request_participants_user_idx ON request_participants (user_id);

-- Agents' queues: named, ordered JQL views of a service project.
CREATE TABLE service_queues (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name       text NOT NULL,
    jql        text NOT NULL,
    position   integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX service_queues_project_idx ON service_queues (project_id, position);

-- When SLA clocks run: working hours ([{day, start, end}], day 0 = Sunday, "09:00") in a time
-- zone, with holidays off. No hours means around the clock.
CREATE TABLE sla_calendars (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name       text NOT NULL,
    time_zone  text NOT NULL DEFAULT 'UTC',
    hours      jsonb NOT NULL DEFAULT '[]',
    holidays   date[] NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX sla_calendars_name_key ON sla_calendars (project_id, lower(name));

-- A service project's SLAs: time to first response and time to resolution, each with the
-- statuses that pause its clock.
CREATE TABLE sla_metrics (
    project_id       uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    metric           text NOT NULL CHECK (metric IN ('first_response', 'resolution')),
    name             text NOT NULL,
    pause_status_ids uuid[] NOT NULL DEFAULT '{}',
    PRIMARY KEY (project_id, metric)
);

-- Goals in order: the first whose conditions all match a request sets its target (empty
-- conditions match every request).
CREATE TABLE sla_goals (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id       uuid NOT NULL,
    metric           text NOT NULL,
    position         integer NOT NULL DEFAULT 0,
    priorities       text[] NOT NULL DEFAULT '{}',
    request_type_ids uuid[] NOT NULL DEFAULT '{}',
    jql              text NOT NULL DEFAULT '',
    target_minutes   integer NOT NULL CHECK (target_minutes > 0),
    calendar_id      uuid REFERENCES sla_calendars (id) ON DELETE SET NULL,
    FOREIGN KEY (project_id, metric) REFERENCES sla_metrics (project_id, metric) ON DELETE CASCADE
);
CREATE INDEX sla_goals_project_idx ON sla_goals (project_id, metric, position);

-- Each request's SLA clocks, recomputed from its history whenever it changes. A running clock's
-- breach_at says when it breaches; the periodic SLA job marks breaches and raises sla.breached.
CREATE TABLE issue_slas (
    issue_id           uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    metric             text NOT NULL CHECK (metric IN ('first_response', 'resolution')),
    project_id         uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    goal_id            uuid REFERENCES sla_goals (id) ON DELETE SET NULL,
    -- NULL: around the clock.
    calendar_id        uuid REFERENCES sla_calendars (id) ON DELETE SET NULL,
    target_seconds     bigint NOT NULL,
    state              text NOT NULL CHECK (state IN ('running', 'paused', 'stopped')),
    -- Working time used by computed_at (running), at the pause, or at the stop.
    elapsed_seconds    bigint NOT NULL,
    cycle_started_at   timestamptz NOT NULL,
    stopped_at         timestamptz,
    breach_at          timestamptz,
    breached           boolean NOT NULL DEFAULT false,
    breached_at        timestamptz,
    breach_notified_at timestamptz,
    computed_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, metric)
);
CREATE INDEX issue_slas_project_idx ON issue_slas (project_id);
CREATE INDEX issue_slas_due_idx ON issue_slas (breach_at) WHERE state = 'running' AND breach_notified_at IS NULL;

-- An approval a request waits for: any one approver, or all of them, must approve.
CREATE TABLE request_approvals (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id        uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    mode            text NOT NULL CHECK (mode IN ('any', 'all')),
    status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'declined')),
    -- The statuses the decision moves the request to (names, as configured on its request type).
    approved_status text NOT NULL,
    declined_status text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    decided_at      timestamptz
);
CREATE INDEX request_approvals_issue_idx ON request_approvals (issue_id);

-- The people asked (a group's members as of the request), and their decisions.
CREATE TABLE approval_approvers (
    approval_id uuid NOT NULL REFERENCES request_approvals (id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    decision    text CHECK (decision IN ('approved', 'declined')),
    comment     text NOT NULL DEFAULT '',
    decided_at  timestamptz,
    PRIMARY KEY (approval_id, user_id)
);
CREATE INDEX approval_approvers_user_idx ON approval_approvers (user_id);

-- +goose Down
DROP TABLE approval_approvers, request_approvals, issue_slas, sla_goals, sla_metrics, sla_calendars,
    service_queues, request_participants, service_requests, request_types, service_customers, service_desks;
UPDATE roles SET permissions = array_remove(permissions, 'customer.manage') WHERE builtin;
ALTER TABLE attachments DROP COLUMN public;
ALTER TABLE comments DROP COLUMN internal;
UPDATE users SET org_role = 'guest' WHERE org_role = 'customer';
ALTER TABLE users DROP CONSTRAINT users_org_role_check,
    ADD CONSTRAINT users_org_role_check CHECK (org_role IN ('admin', 'member', 'guest'));
