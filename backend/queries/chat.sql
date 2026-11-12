-- name: SetUserAvailable :one
UPDATE users
SET available = ?
WHERE id = ?
RETURNING available;

-- name: ListFreshPartnerIDs :many
-- Available users with no chat against me. The caller picks one at random.
SELECT u.id
FROM users AS me
JOIN users AS u
  ON u.id != me.id
 AND u.available != 0
 AND NOT EXISTS (
    SELECT 1
    FROM chats AS c
    WHERE (c.user_low = u.id AND c.user_high = me.id)
       OR (c.user_low = me.id AND c.user_high = u.id)
 )
WHERE me.id = ?
ORDER BY u.id;

-- name: ListRematchChats :many
-- Chats I already have whose other member is available.
SELECT c.id, c.user_low, c.user_high, c.created_at, c.last_message_at
FROM users AS me
JOIN chats AS c
  ON c.user_low = me.id OR c.user_high = me.id
JOIN users AS partner
  ON partner.id = CASE
    WHEN c.user_low = me.id THEN c.user_high
    ELSE c.user_low
  END
WHERE me.id = ?
  AND partner.available != 0
ORDER BY c.id;

-- name: InsertChat :one
INSERT INTO chats (user_low, user_high, created_at, last_message_at)
VALUES (?, ?, ?, ?)
RETURNING id, user_low, user_high, created_at, last_message_at;

-- name: InsertMessage :one
INSERT INTO messages (chat_id, sender_id, kind, body, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING id, chat_id, sender_id, kind, body, created_at;

-- name: TouchChat :exec
UPDATE chats
SET last_message_at = ?
WHERE id = ?;

-- name: GetChatByID :one
SELECT id, user_low, user_high, created_at, last_message_at
FROM chats
WHERE id = ?;

-- name: ListChatsForUser :many
SELECT
    c.id,
    partner.id AS partner_id,
    partner.name AS partner_name,
    partner.avatar_path AS partner_avatar_path,
    lm.id AS last_message_id,
    lm.body AS last_body,
    lm.kind AS last_kind,
    lm.created_at AS last_created_at,
    (
        SELECT count(*)
        FROM messages AS m
        WHERE m.chat_id = c.id
          AND m.id > COALESCE((
            SELECT cr.last_read_message_id
            FROM chat_reads AS cr
            WHERE cr.chat_id = c.id AND cr.user_id = me.id
          ), 0)
          AND (m.sender_id IS NULL OR m.sender_id != me.id)
    ) AS unread_count
FROM users AS me
JOIN chats AS c
  ON c.user_low = me.id OR c.user_high = me.id
JOIN users AS partner
  ON partner.id = CASE
    WHEN c.user_low = me.id THEN c.user_high
    ELSE c.user_low
  END
LEFT JOIN messages AS lm
  ON lm.chat_id = c.id
 AND lm.id = (
    SELECT max(m2.id)
    FROM messages AS m2
    WHERE m2.chat_id = c.id
 )
WHERE me.id = ?
ORDER BY c.last_message_at DESC, c.id DESC;

-- name: ListLatestMessages :many
SELECT id, chat_id, sender_id, kind, body, created_at
FROM messages
WHERE chat_id = ?
ORDER BY id DESC
LIMIT ?;

-- name: ListMessagesBefore :many
SELECT id, chat_id, sender_id, kind, body, created_at
FROM messages
WHERE chat_id = ? AND id < ?
ORDER BY id DESC
LIMIT ?;

-- name: GetMessageInChat :one
SELECT id, chat_id, sender_id, kind, body, created_at
FROM messages
WHERE id = ? AND chat_id = ?;

-- name: AdvanceChatRead :one
-- Monotonic: a smaller id leaves the stored cursor where it is.
INSERT INTO chat_reads (chat_id, user_id, last_read_message_id)
VALUES (?, ?, ?)
ON CONFLICT (chat_id, user_id) DO UPDATE SET
    last_read_message_id = CASE
        WHEN excluded.last_read_message_id > chat_reads.last_read_message_id
        THEN excluded.last_read_message_id
        ELSE chat_reads.last_read_message_id
    END
RETURNING last_read_message_id;
