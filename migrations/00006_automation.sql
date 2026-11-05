-- +goose Up
-- +goose StatementBegin
ALTER TABLE releases ADD COLUMN polymaths_task_id text NOT NULL DEFAULT '';
ALTER TABLE releases ADD COLUMN moderation_revision bigint NOT NULL DEFAULT 0;
CREATE TABLE story_registrations (
    channel_id text NOT NULL REFERENCES channels(id),
    registration_key text NOT NULL,
    event_key text NOT NULL,
    development_id uuid NOT NULL REFERENCES story_developments(id) ON DELETE CASCADE,
    PRIMARY KEY(channel_id, registration_key)
);
CREATE TABLE rework_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    release_id uuid NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    task_id text NOT NULL DEFAULT '',
    note text NOT NULL,
    note_hash text NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','dispatching','sent','needs_human','cancelled')),
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_token uuid,
    lease_until timestamptz,
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(release_id, note_hash)
);
CREATE INDEX rework_jobs_pending_idx ON rework_jobs(available_at) WHERE status IN ('pending','dispatching');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE rework_jobs;
DROP TABLE story_registrations;
ALTER TABLE releases DROP COLUMN polymaths_task_id;
ALTER TABLE releases DROP COLUMN moderation_revision;
-- +goose StatementEnd
