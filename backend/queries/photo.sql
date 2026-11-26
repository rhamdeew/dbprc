-- name: GetPhotoOwner :one
SELECT user_id FROM photos WHERE id = ?;

-- name: AddPhotoLike :exec
INSERT INTO photo_likes (photo_id, user_id, created_at)
VALUES (?, ?, ?)
ON CONFLICT (photo_id, user_id) DO NOTHING;

-- name: RemovePhotoLike :exec
DELETE FROM photo_likes WHERE photo_id = ? AND user_id = ?;

-- name: GetPhotoLikes :one
SELECT count(*) AS like_count,
       CAST(coalesce(max(user_id = sqlc.arg(viewer_id)), 0) AS INTEGER) AS liked_by_me
FROM photo_likes
WHERE photo_id = sqlc.arg(photo_id);

-- name: ListPhotoComments :many
SELECT recent.id, recent.author_id, recent.body, recent.created_at, users.name, users.avatar_path
FROM (
    SELECT id, author_id, body, created_at
    FROM photo_comments
    WHERE photo_id = ?
    ORDER BY id DESC
    LIMIT 200
) AS recent
JOIN users ON users.id = recent.author_id
ORDER BY recent.id;

-- name: CreatePhotoComment :one
INSERT INTO photo_comments (photo_id, author_id, body, created_at)
VALUES (?, ?, ?, ?)
RETURNING id;

-- name: DeletePhotoComment :one
DELETE FROM photo_comments
WHERE photo_comments.id = sqlc.arg(id) AND photo_id = sqlc.arg(photo_id)
  AND (author_id = sqlc.arg(viewer_id)
       OR sqlc.arg(viewer_id) = (SELECT user_id FROM photos WHERE photos.id = sqlc.arg(photo_id)))
RETURNING id;
