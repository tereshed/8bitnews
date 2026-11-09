-- +goose Up
-- Publishing without a per-video approval (docs/automation.md): the slot a
-- release holds on its channel, what hub has found out about its upload, and
-- the reason a release is kept back from the queue.
-- +goose StatementBegin
CREATE TABLE release_slots (
    release_id uuid PRIMARY KEY REFERENCES releases (id) ON DELETE CASCADE,
    channel_id text NOT NULL REFERENCES channels (id),
    publish_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (channel_id, publish_at)
);
-- +goose StatementEnd

-- live_at: hub has settled whether the video is up, by seeing it or by a
-- moderator's press; after that a moderator's «↩️ Не выложено» stays as it is.
-- +goose StatementBegin
ALTER TABLE release_publications
    ADD COLUMN live_at            timestamptz,
    ADD COLUMN overdue_alerted_at timestamptz;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE release_publications p SET live_at = r.updated_at
FROM releases r
WHERE r.id = p.release_id AND r.status = 'published';
-- +goose StatementEnd

-- A release a moderator took back with «↩️» before this migration stays back.
-- +goose StatementBegin
UPDATE release_publications p SET live_at = now()
WHERE p.live_at IS NULL AND EXISTS (
    SELECT 1 FROM events e
    WHERE e.kind = 'moderation' AND e.data ->> 'action' = 'unpublished'
      AND e.data ->> 'release_id' = p.release_id::text
);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE releases
    ADD COLUMN publish_hold      text NOT NULL DEFAULT '',
    ADD COLUMN upload_alerted_at timestamptz;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE releases
    DROP COLUMN IF EXISTS upload_alerted_at,
    DROP COLUMN IF EXISTS publish_hold;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE release_publications
    DROP COLUMN IF EXISTS overdue_alerted_at,
    DROP COLUMN IF EXISTS live_at;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS release_slots;
-- +goose StatementEnd
