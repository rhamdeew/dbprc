-- name: GetInterests :one
SELECT user_id, hobby, music, movies, other, updated_at
FROM interests
WHERE user_id = ?;

-- name: UpsertInterests :one
INSERT INTO interests (user_id, hobby, music, movies, other, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (user_id) DO UPDATE SET
    hobby = excluded.hobby,
    music = excluded.music,
    movies = excluded.movies,
    other = excluded.other,
    updated_at = excluded.updated_at
RETURNING user_id, hobby, music, movies, other, updated_at;

-- name: ListUserPhotos :many
SELECT id, user_id, path, created_at
FROM photos
WHERE user_id = ?
ORDER BY id;

-- name: CountUserPhotos :one
SELECT count(*) AS n
FROM photos
WHERE user_id = ?;

-- name: CreatePhoto :one
INSERT INTO photos (user_id, path, created_at)
VALUES (?, ?, ?)
RETURNING id, user_id, path, created_at;

-- name: DeleteUserPhoto :one
DELETE FROM photos
WHERE id = ? AND user_id = ?
RETURNING path;

-- name: UpdateAvatarPath :one
UPDATE users
SET avatar_path = ?
WHERE id = ?
RETURNING id, google_sub, email, name, avatar_path, available, created_at, last_login_at;

-- name: SearchUsers :many
-- pattern is a LIKE prefix: \ % and _ already escaped, with a trailing %.
-- like(pattern, text, escape) is LIKE ... ESCAPE (sqlc's parser rejects the ESCAPE keyword).
WITH pattern AS (
    SELECT lower(sqlc.arg('pattern')) AS value
)
SELECT id, name, email, avatar_path
FROM users, pattern
WHERE like(pattern.value, lower(email), char(92))
   OR like(pattern.value, lower(name), char(92))
   OR like('% ' || pattern.value, lower(name), char(92))
ORDER BY lower(name), id
LIMIT sqlc.arg('row_limit');
