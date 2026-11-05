-- +goose Up
-- What videos cost, for the channel's public costs page. hub keeps its own copy
-- because PolyMaths drops a run's token usage after 30 days.
--
-- cost_runs: one Newsroom run (a PolyMaths root task with its subtasks), in
-- tokens: the agents run on a flat-rate subscription, so a run has no price of
-- its own. task_id is internal and never goes on a page. complete is false when
-- the figures are known to fall short: the run couldn't be read, or was first
-- read after PolyMaths had started forgetting it.
-- +goose StatementBegin
CREATE TABLE cost_runs (
    task_id            text PRIMARY KEY,
    channel_id         text NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    development_id     uuid REFERENCES story_developments (id) ON DELETE SET NULL,
    started_at         timestamptz NOT NULL,
    agent_runs         integer NOT NULL DEFAULT 0,
    measured_runs      integer NOT NULL DEFAULT 0,
    input_tokens       bigint NOT NULL DEFAULT 0,
    output_tokens      bigint NOT NULL DEFAULT 0,
    cache_write_tokens bigint NOT NULL DEFAULT 0,
    cache_read_tokens  bigint NOT NULL DEFAULT 0,
    stages             jsonb NOT NULL DEFAULT '{}',
    complete           boolean NOT NULL DEFAULT true,
    source_updated_at  timestamptz NOT NULL,
    measured_at        timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX cost_runs_channel_idx ON cost_runs (channel_id);
-- +goose StatementEnd

-- cost_builds: one video build's pictures, in dollars: paid per request, and the
-- build's package says what the provider charged. One row per package archive,
-- so a rebuild adds to its development's cost. images_usd is NULL for a package
-- that can't be read. counted is false for that, and for a second copy of a build
-- already counted: a retried upload stores the same package under a new key.
-- +goose StatementBegin
CREATE TABLE cost_builds (
    object_key     text PRIMARY KEY,
    development_id uuid NOT NULL REFERENCES story_developments (id) ON DELETE CASCADE,
    images_usd     numeric(10, 4) CHECK (images_usd >= 0),
    package_sha256 text NOT NULL DEFAULT '',
    counted        boolean NOT NULL DEFAULT true,
    read_at        timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX cost_builds_development_idx ON cost_builds (development_id);
-- +goose StatementEnd

-- cost_sync: when each source was last read in full, so the page's "updated"
-- date can't vouch for figures whose source has stopped answering.
-- +goose StatementBegin
CREATE TABLE cost_sync (
    source text PRIMARY KEY,
    ok_at  timestamptz NOT NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS cost_sync;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS cost_builds;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS cost_runs;
-- +goose StatementEnd
