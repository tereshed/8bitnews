-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pgcrypto;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pg_trgm;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE countries (
    cc         text PRIMARY KEY,
    name       text NOT NULL,
    language   text NOT NULL,
    policy_md  text NOT NULL DEFAULT '',
    legal_md   text NOT NULL DEFAULT '',
    data       jsonb NOT NULL DEFAULT '{}',
    updated_at timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE channels (
    id           text PRIMARY KEY,
    country_cc   text NOT NULL REFERENCES countries (cc),
    name         text NOT NULL,
    language     text NOT NULL,
    timezone     text NOT NULL DEFAULT '',
    platforms    jsonb NOT NULL DEFAULT '{}',
    format       jsonb NOT NULL DEFAULT '{}',
    format_md    text NOT NULL DEFAULT '',
    metadata_md  text NOT NULL DEFAULT '',
    data         jsonb NOT NULL DEFAULT '{}',
    updated_at   timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE platform_accounts (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id           text NOT NULL REFERENCES channels (id),
    platform             text NOT NULL,
    external_account_id  text NOT NULL DEFAULT '',
    oauth_tokens         jsonb NOT NULL DEFAULT '{}',
    created_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (channel_id, platform)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE stories (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id  text NOT NULL REFERENCES channels (id),
    title       text NOT NULL,
    summary     text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX stories_channel_id_idx ON stories (channel_id);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX stories_title_trgm_idx ON stories USING gin (title gin_trgm_ops);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX stories_summary_trgm_idx ON stories USING gin (summary gin_trgm_ops);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE story_developments (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    story_id           uuid NOT NULL REFERENCES stories (id),
    summary            text NOT NULL,
    reported_at        timestamptz NOT NULL,
    seed_urls          jsonb NOT NULL DEFAULT '[]',
    polymaths_task_id  text NOT NULL DEFAULT '',
    created_at         timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX story_developments_story_id_idx ON story_developments (story_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE experiments (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id  text NOT NULL REFERENCES channels (id),
    factor      text NOT NULL,
    status      text NOT NULL DEFAULT 'draft',
    created_at  timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE experiment_arms (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    experiment_id  uuid NOT NULL REFERENCES experiments (id),
    arm_id         text NOT NULL,
    params         jsonb NOT NULL DEFAULT '{}',
    UNIQUE (experiment_id, arm_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE assignments (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    development_id  uuid NOT NULL REFERENCES story_developments (id),
    channel_id      text NOT NULL REFERENCES channels (id),
    experiment_id   uuid REFERENCES experiments (id),
    arm_id          text NOT NULL,
    params          jsonb NOT NULL DEFAULT '{}',
    created_at      timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX assignments_development_id_idx ON assignments (development_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE releases (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    development_id     uuid NOT NULL REFERENCES story_developments (id),
    channel_id         text NOT NULL REFERENCES channels (id),
    experiment_id      uuid REFERENCES experiments (id),
    arm_id             text NOT NULL DEFAULT 'default',
    status             text NOT NULL DEFAULT 'submitted'
        CHECK (status IN (
            'submitted', 'pending_approval', 'approved', 'scheduled', 'published',
            'rejected', 'changes_requested', 'failed'
        )),
    media              jsonb NOT NULL DEFAULT '{}',
    research_dossier   jsonb NOT NULL DEFAULT '{}',
    script             jsonb NOT NULL DEFAULT '{}',
    video_package      jsonb NOT NULL DEFAULT '{}',
    validation_report  jsonb NOT NULL DEFAULT '{}',
    legal_review       jsonb NOT NULL DEFAULT '{}',
    allowed_platforms  text[] NOT NULL DEFAULT '{}',
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX releases_development_id_idx ON releases (development_id);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX releases_channel_id_idx ON releases (channel_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE release_sources (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    release_id        uuid NOT NULL REFERENCES releases (id),
    url               text NOT NULL,
    final_url         text NOT NULL DEFAULT '',
    outlet            text NOT NULL DEFAULT '',
    title             text NOT NULL DEFAULT '',
    published_at      text NOT NULL DEFAULT '',
    fact_ids          text[] NOT NULL DEFAULT '{}',
    is_primary        boolean NOT NULL DEFAULT false,
    paywall           boolean NOT NULL DEFAULT false,
    state_media       boolean NOT NULL DEFAULT false,
    check_status      text NOT NULL,
    http_status       int,
    archive_url       text NOT NULL DEFAULT '',
    text_fingerprint  text NOT NULL DEFAULT '',
    checked_at        timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX release_sources_release_id_idx ON release_sources (release_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE publications (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    release_id            uuid NOT NULL REFERENCES releases (id),
    platform_account_id   uuid REFERENCES platform_accounts (id),
    platform              text NOT NULL,
    external_id           text NOT NULL DEFAULT '',
    url                   text NOT NULL DEFAULT '',
    status                text NOT NULL DEFAULT 'pending',
    published_at          timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX publications_release_id_idx ON publications (release_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE metric_snapshots (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    publication_id      uuid NOT NULL REFERENCES publications (id),
    captured_at         timestamptz NOT NULL DEFAULT now(),
    age_minutes         int NOT NULL,
    views               bigint NOT NULL DEFAULT 0,
    likes               bigint NOT NULL DEFAULT 0,
    comments            bigint NOT NULL DEFAULT 0,
    shares              bigint NOT NULL DEFAULT 0,
    saves               bigint NOT NULL DEFAULT 0,
    watch_time_seconds  bigint NOT NULL DEFAULT 0,
    avg_pct_viewed      double precision,
    followers_delta     bigint NOT NULL DEFAULT 0,
    raw                 jsonb NOT NULL DEFAULT '{}'
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX metric_snapshots_publication_id_idx ON metric_snapshots (publication_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE music_tracks (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        text NOT NULL UNIQUE,
    data        jsonb NOT NULL DEFAULT '{}',
    active      boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE platform_incidents (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id  text NOT NULL REFERENCES channels (id),
    platform    text NOT NULL,
    kind        text NOT NULL,
    data        jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX platform_incidents_channel_id_idx ON platform_incidents (channel_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE events (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind        text NOT NULL,
    data        jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX events_kind_idx ON events (kind);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS events;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS platform_incidents;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS music_tracks;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS metric_snapshots;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS publications;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS release_sources;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS releases;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS assignments;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS experiment_arms;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS experiments;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS story_developments;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS stories;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS platform_accounts;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS channels;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS countries;
-- +goose StatementEnd
