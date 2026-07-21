-- +goose Up
-- +goose StatementBegin
CREATE TABLE model_health (
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    harness TEXT NOT NULL,
    model TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('unknown', 'reachable', 'unreachable')),
    reason TEXT NOT NULL CHECK (reason IN ('not-probed', 'no-capability', 'probe-unavailable', 'reachable', 'unreachable', 'recovered')),
    message TEXT NOT NULL DEFAULT '',
    observed_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    PRIMARY KEY (project_id, harness, model)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS model_health;
-- +goose StatementEnd
