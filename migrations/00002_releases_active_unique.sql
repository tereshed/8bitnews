-- +goose Up
-- +goose StatementBegin
CREATE UNIQUE INDEX releases_development_id_active_idx ON releases (development_id)
    WHERE status NOT IN ('rejected', 'changes_requested', 'failed');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS releases_development_id_active_idx;
-- +goose StatementEnd
