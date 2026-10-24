-- +goose Up
-- Email notification settings, one row per person who changed a default or received a digest.
-- People without a row get the defaults in internal/notify (assignments immediately, the rest
-- in a daily digest at 09:00 UTC).
CREATE TABLE email_notification_settings (
    user_id        uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    -- Delivery per notification kind, e.g. {"commented": "off"}: immediate | digest | off.
    -- Kinds not listed use their default.
    deliveries     jsonb NOT NULL DEFAULT '{}',
    -- Local hour (in time_zone) at which the daily digest goes out.
    digest_hour    smallint NOT NULL DEFAULT 9 CHECK (digest_hour BETWEEN 0 AND 23),
    -- IANA time zone name, e.g. Europe/London.
    time_zone      text NOT NULL DEFAULT 'UTC',
    -- Pause every notification email (in-app notifications continue).
    paused         boolean NOT NULL DEFAULT false,
    last_digest_at timestamptz,
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- How an in-app notification is emailed, decided from the recipient's settings when it is
-- created: immediately (several on one issue within a short window become one email), in the
-- daily digest, or not at all (NULL). emailed_at is set once it was sent or skipped.
ALTER TABLE notifications
    ADD COLUMN email_delivery text CHECK (email_delivery IN ('immediate', 'digest')),
    ADD COLUMN emailed_at timestamptz;
CREATE INDEX notifications_email_pending_idx ON notifications (email_delivery, user_id, issue_id)
    WHERE email_delivery IS NOT NULL AND emailed_at IS NULL;

-- +goose Down
DROP INDEX notifications_email_pending_idx;
ALTER TABLE notifications DROP COLUMN emailed_at, DROP COLUMN email_delivery;
DROP TABLE email_notification_settings;
