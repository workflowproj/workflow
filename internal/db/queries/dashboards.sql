-- name: ListDashboards :many
-- The viewer's dashboards first, then those shared by others.
SELECT * FROM dashboards
WHERE owner_id = @user_id OR shared
ORDER BY (owner_id = @user_id) DESC, lower(name), created_at;

-- name: GetDashboard :one
SELECT * FROM dashboards WHERE id = $1;

-- name: CreateDashboard :one
INSERT INTO dashboards (owner_id, name, description, shared, gadgets)
VALUES (@owner_id, @name, @description, @shared, @gadgets)
RETURNING *;

-- name: UpdateDashboard :one
UPDATE dashboards SET name = @name, description = @description, shared = @shared, gadgets = @gadgets, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteDashboard :exec
DELETE FROM dashboards WHERE id = $1;
