-- +goose Up
-- Complete ARC schema. Later features add queries, not tables.
-- Timestamps are TEXT RFC3339 UTC, written by the application.

CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    google_sub TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL COLLATE NOCASE UNIQUE,
    name TEXT NOT NULL,
    avatar_path TEXT,
    available INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    last_login_at TEXT NOT NULL
);

CREATE TABLE interests (
    user_id INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    hobby TEXT NOT NULL DEFAULT '',
    music TEXT NOT NULL DEFAULT '',
    movies TEXT NOT NULL DEFAULT '',
    other TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);

CREATE TABLE photos (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX photos_user_id_idx ON photos (user_id);

-- sessions matches github.com/alexedwards/scs/sqlite3store.
CREATE TABLE sessions (
    token TEXT PRIMARY KEY,
    data BLOB NOT NULL,
    expiry REAL NOT NULL
);

CREATE INDEX sessions_expiry_idx ON sessions (expiry);

CREATE TABLE wall_posts (
    id INTEGER PRIMARY KEY,
    wall_owner_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    author_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    parent_id INTEGER REFERENCES wall_posts (id) ON DELETE CASCADE,
    body TEXT NOT NULL DEFAULT '',
    drawing_path TEXT,
    is_anonymous INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);

CREATE INDEX wall_posts_wall_owner_id_parent_id_created_at_idx
    ON wall_posts (wall_owner_id, parent_id, created_at);

CREATE TABLE chats (
    id INTEGER PRIMARY KEY,
    user_low INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    user_high INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    last_message_at TEXT NOT NULL,
    UNIQUE (user_low, user_high),
    CHECK (user_low < user_high)
);

CREATE TABLE messages (
    id INTEGER PRIMARY KEY,
    chat_id INTEGER NOT NULL REFERENCES chats (id) ON DELETE CASCADE,
    sender_id INTEGER REFERENCES users (id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('text', 'system')),
    body TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX messages_chat_id_id_idx ON messages (chat_id, id);

CREATE TABLE chat_reads (
    chat_id INTEGER NOT NULL REFERENCES chats (id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    last_read_message_id INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (chat_id, user_id)
);

-- No foreign keys: audit rows stay after the target user is deleted.
CREATE TABLE admin_audit (
    id INTEGER PRIMARY KEY,
    admin_email TEXT,
    action TEXT,
    target_email_sha256 TEXT,
    created_at TEXT NOT NULL
);

-- +goose Down

DROP TABLE admin_audit;
DROP TABLE chat_reads;
DROP TABLE messages;
DROP TABLE chats;
DROP TABLE wall_posts;
DROP TABLE sessions;
DROP TABLE photos;
DROP TABLE interests;
DROP TABLE users;
