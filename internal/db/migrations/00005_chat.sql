-- +goose Up
-- Slack and Mattermost channels that receive project notifications through incoming webhooks.
CREATE TABLE chat_channels (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    provider        text NOT NULL CHECK (provider IN ('slack', 'mattermost')),
    name            text NOT NULL,
    webhook_url_enc text NOT NULL,           -- sealed; the incoming webhook URL is a credential
    url_hint        text NOT NULL,           -- shown instead of the URL, e.g. hooks.slack.com/…/a1b2
    events          text[] NOT NULL,
    jql             text NOT NULL DEFAULT '',
    active          boolean NOT NULL DEFAULT true,
    created_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX chat_channels_project_idx ON chat_channels (project_id);

CREATE TABLE chat_deliveries (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id      uuid NOT NULL REFERENCES chat_channels (id) ON DELETE CASCADE,
    event           text NOT NULL,
    issue_key       text,
    payload         jsonb NOT NULL,
    status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'failed')),
    attempts        integer NOT NULL DEFAULT 0,
    response_status integer,
    last_error      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    delivered_at    timestamptz
);
CREATE INDEX chat_deliveries_channel_idx ON chat_deliveries (channel_id, created_at DESC);

-- +goose Down
DROP TABLE chat_deliveries, chat_channels;
