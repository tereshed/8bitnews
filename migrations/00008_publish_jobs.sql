-- +goose Up
-- hub uploading to YouTube itself (docs/automation.md): how far each upload has
-- got, so a restart resumes it instead of uploading twice, and whether the
-- group has been told a channel lost its access.
-- +goose StatementBegin
CREATE TABLE publish_jobs (
    release_id         uuid PRIMARY KEY REFERENCES releases (id) ON DELETE CASCADE,
    session_uri        text NOT NULL DEFAULT '',
    video_size         bigint NOT NULL DEFAULT 0,
    publish_at         timestamptz,
    video_id           text NOT NULL DEFAULT '',
    thumbnail_set      boolean NOT NULL DEFAULT false,
    thumbnail_attempts integer NOT NULL DEFAULT 0,
    attempts           integer NOT NULL DEFAULT 0,
    next_attempt_at    timestamptz NOT NULL DEFAULT now(),
    last_error         text NOT NULL DEFAULT '',
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE platform_accounts ADD COLUMN auth_alerted_at timestamptz;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE platform_accounts DROP COLUMN IF EXISTS auth_alerted_at;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS publish_jobs;
-- +goose StatementEnd
