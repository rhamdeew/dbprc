-- name: CountUsers :one
SELECT count(*) AS n
FROM users;

-- name: CountAvailableUsers :one
SELECT count(*) AS n
FROM users
WHERE available != 0;

-- name: CountChats :one
SELECT count(*) AS n
FROM chats;

-- name: CountMessages :one
SELECT count(*) AS n
FROM messages;

-- name: CountWallPosts :one
SELECT count(*) AS n
FROM wall_posts;

-- name: CountPhotos :one
SELECT count(*) AS n
FROM photos;

-- name: CountWallPostsByOwner :one
SELECT count(*) AS n
FROM wall_posts
WHERE wall_owner_id = ?;

-- name: CountWallPostsByAuthor :one
SELECT count(*) AS n
FROM wall_posts
WHERE author_id = ?;

-- name: CountChatsByUser :one
SELECT count(*) AS n
FROM chats
WHERE user_low = sqlc.arg('user_id') OR user_high = sqlc.arg('user_id');

-- name: CountMessagesByUser :one
SELECT count(*) AS n
FROM messages
WHERE chat_id IN (
    SELECT id FROM chats
    WHERE user_low = sqlc.arg('user_id') OR user_high = sqlc.arg('user_id')
);

-- name: ListAvatarPath :one
SELECT avatar_path
FROM users
WHERE id = ?;

-- name: ListPhotoPathsByUser :many
SELECT path
FROM photos
WHERE user_id = ?;

-- name: ListDrawingPathsByUser :many
SELECT p.drawing_path
FROM wall_posts p
WHERE p.drawing_path IS NOT NULL
  AND p.drawing_path != ''
  AND (
    p.wall_owner_id = sqlc.arg('user_id')
    OR p.author_id = sqlc.arg('user_id')
    -- Replies other people wrote under this user's posts on a third wall.
    -- The delete cascades to those rows through parent_id.
    OR p.parent_id IN (SELECT r.id FROM wall_posts r WHERE r.author_id = sqlc.arg('user_id'))
  );

-- name: ListSessionRows :many
SELECT token, data
FROM sessions;

-- name: DeleteSessionByToken :exec
DELETE FROM sessions
WHERE token = ?;

-- name: InsertAdminAudit :exec
INSERT INTO admin_audit (admin_email, action, target_email_sha256, created_at)
VALUES (?, ?, ?, ?);
