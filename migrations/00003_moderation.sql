-- +goose Up
-- Moderation in the Telegram group (docs/architecture.md §6, E4): the card the
-- bot posted for a release, and the moderator's decision and its reason.
-- +goose StatementBegin
ALTER TABLE releases
    ADD COLUMN moderation_chat_id    bigint,
    ADD COLUMN moderation_message_id bigint,
    ADD COLUMN moderated_by          text NOT NULL DEFAULT '',
    ADD COLUMN moderated_at          timestamptz,
    ADD COLUMN moderation_note       text NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE releases
    DROP COLUMN IF EXISTS moderation_note,
    DROP COLUMN IF EXISTS moderated_at,
    DROP COLUMN IF EXISTS moderated_by,
    DROP COLUMN IF EXISTS moderation_message_id,
    DROP COLUMN IF EXISTS moderation_chat_id;
-- +goose StatementEnd
