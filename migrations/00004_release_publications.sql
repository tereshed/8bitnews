-- +goose Up
-- The admin page. Where a release went up: the link on each platform, set by
-- hand, since uploads are by hand for now (metadata.md). And a number each
-- moderator's sessions must carry: signing out raises it, ending them all.
-- +goose StatementBegin
CREATE TABLE release_publications (
    release_id uuid NOT NULL REFERENCES releases (id) ON DELETE CASCADE,
    platform   text NOT NULL,
    url        text NOT NULL,
    set_by     text NOT NULL DEFAULT '',
    set_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (release_id, platform)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE admin_session_epochs (
    telegram_user_id bigint PRIMARY KEY,
    epoch            integer NOT NULL DEFAULT 0,
    updated_at       timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS admin_session_epochs;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS release_publications;
-- +goose StatementEnd
