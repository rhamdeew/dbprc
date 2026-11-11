-- name: GetWallPost :one
SELECT id, wall_owner_id, parent_id, created_at
FROM wall_posts
WHERE id = ?;

-- name: CreateWallPost :one
INSERT INTO wall_posts (
    wall_owner_id,
    author_id,
    parent_id,
    body,
    drawing_path,
    is_anonymous,
    created_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id, body, drawing_path, is_anonymous, created_at;

-- name: ListRootWallPosts :many
SELECT
    p.id,
    p.body,
    p.drawing_path,
    p.is_anonymous,
    p.created_at,
    p.author_id,
    u.name AS author_name,
    u.avatar_path AS author_avatar_path
FROM wall_posts p
JOIN users u ON u.id = p.author_id
WHERE p.wall_owner_id = ?
  AND p.parent_id IS NULL
ORDER BY p.created_at DESC, p.id DESC
LIMIT ?;

-- name: ListRootWallPostsBefore :many
SELECT
    p.id,
    p.body,
    p.drawing_path,
    p.is_anonymous,
    p.created_at,
    p.author_id,
    u.name AS author_name,
    u.avatar_path AS author_avatar_path
FROM wall_posts p
JOIN users u ON u.id = p.author_id
WHERE p.wall_owner_id = ?
  AND p.parent_id IS NULL
  AND (
    p.created_at < ?
    OR (p.created_at = ? AND p.id < ?)
  )
ORDER BY p.created_at DESC, p.id DESC
LIMIT ?;

-- name: ListWallReplies :many
SELECT
    p.id,
    p.parent_id,
    p.body,
    p.drawing_path,
    p.is_anonymous,
    p.created_at,
    p.author_id,
    u.name AS author_name,
    u.avatar_path AS author_avatar_path
FROM wall_posts p
JOIN users u ON u.id = p.author_id
WHERE p.wall_owner_id = ?
  AND p.parent_id IN (sqlc.slice('parent_ids'))
ORDER BY p.created_at ASC, p.id ASC;

-- name: ListDrawingPathsForDelete :many
SELECT p.drawing_path
FROM wall_posts p
WHERE p.drawing_path IS NOT NULL
  AND p.drawing_path != ''
  AND (
    p.id = sqlc.arg('id')
    OR p.parent_id = sqlc.arg('id')
    OR p.parent_id IN (SELECT c.id FROM wall_posts c WHERE c.parent_id = sqlc.arg('id'))
  );

-- name: DeleteWallPost :execrows
DELETE FROM wall_posts
WHERE id = ? AND wall_owner_id = ?;
