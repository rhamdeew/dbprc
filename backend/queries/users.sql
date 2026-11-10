-- name: CreateUser :one
INSERT INTO users (
    google_sub,
    email,
    name,
    avatar_path,
    available,
    created_at,
    last_login_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id, google_sub, email, name, avatar_path, available, created_at, last_login_at;

-- name: GetUser :one
SELECT id, google_sub, email, name, avatar_path, available, created_at, last_login_at
FROM users
WHERE id = ?;

-- name: GetUserByEmail :one
SELECT id, google_sub, email, name, avatar_path, available, created_at, last_login_at
FROM users
WHERE email = ?;

-- name: GetUserByGoogleSub :one
SELECT id, google_sub, email, name, avatar_path, available, created_at, last_login_at
FROM users
WHERE google_sub = ?;

-- name: UpdateUser :one
UPDATE users
SET
    email = ?,
    name = ?,
    avatar_path = ?,
    available = ?,
    last_login_at = ?
WHERE id = ?
RETURNING id, google_sub, email, name, avatar_path, available, created_at, last_login_at;

-- name: DeleteUser :exec
DELETE FROM users
WHERE id = ?;
