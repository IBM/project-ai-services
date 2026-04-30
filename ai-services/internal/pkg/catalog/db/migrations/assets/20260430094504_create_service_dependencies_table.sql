-- +goose Up
-- +goose StatementBegin
-- Create service_dependencies table
CREATE TABLE service_dependencies (
    consumer_service_id UUID NOT NULL,
    provider_service_id UUID NOT NULL,
    PRIMARY KEY (consumer_service_id, provider_service_id),
    CONSTRAINT fk_consumer_service_id FOREIGN KEY (consumer_service_id) REFERENCES services(id) ON DELETE CASCADE,
    CONSTRAINT fk_provider_service_id FOREIGN KEY (provider_service_id) REFERENCES services(id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Drop table
DROP TABLE IF EXISTS service_dependencies;
-- +goose StatementEnd
