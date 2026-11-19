-- +goose Up
ALTER TABLE users ADD COLUMN nickname TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN hometown TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN about TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN status_updated_at TEXT;
ALTER TABLE users ADD COLUMN allow_anonymous INTEGER NOT NULL DEFAULT 1 CHECK (allow_anonymous IN (0, 1));

-- +goose Down
ALTER TABLE users DROP COLUMN allow_anonymous;
ALTER TABLE users DROP COLUMN status_updated_at;
ALTER TABLE users DROP COLUMN status;
ALTER TABLE users DROP COLUMN about;
ALTER TABLE users DROP COLUMN hometown;
ALTER TABLE users DROP COLUMN nickname;
