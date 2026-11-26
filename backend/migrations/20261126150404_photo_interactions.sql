-- +goose Up
CREATE TABLE photo_likes (
    photo_id INTEGER NOT NULL REFERENCES photos (id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    PRIMARY KEY (photo_id, user_id)
);

CREATE INDEX photo_likes_user_id_idx ON photo_likes (user_id);

CREATE TABLE photo_comments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    photo_id INTEGER NOT NULL REFERENCES photos (id) ON DELETE CASCADE,
    author_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX photo_comments_photo_id_id_idx ON photo_comments (photo_id, id);
CREATE INDEX photo_comments_author_id_idx ON photo_comments (author_id);

-- +goose Down
DROP TABLE photo_comments;
DROP TABLE photo_likes;
