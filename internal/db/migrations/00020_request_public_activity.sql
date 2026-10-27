-- +goose Up
-- When a service request last changed in a way its customer can see: it was raised, a reply they
-- can see, a status change, or a file added in the help centre. Internal notes and other edits
-- agents make don't count, so the help centre's "last update" never hints at them.
-- +goose StatementBegin
CREATE FUNCTION request_public_activity(request_id uuid, raised timestamptz) RETURNS timestamptz
LANGUAGE sql STABLE PARALLEL SAFE AS $$
    SELECT GREATEST(raised,
        (SELECT max(c.created_at) FROM comments c WHERE c.issue_id = request_id AND NOT c.internal),
        (SELECT max(h.created_at) FROM issue_history h WHERE h.issue_id = request_id AND h.field = 'status'),
        (SELECT max(a.created_at) FROM attachments a WHERE a.issue_id = request_id AND a.public))
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION request_public_activity(uuid, timestamptz);
