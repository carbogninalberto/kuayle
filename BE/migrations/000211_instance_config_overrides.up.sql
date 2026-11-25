-- Settings -> Instance configuration (KUA-287): a sysadmin's value for an
-- environment variable a running server reads per use. A row wins over the
-- environment; deleting it returns to the environment or default. The API
-- validates names and values against its registry (internal/config).
CREATE TABLE instance_config_overrides (
    name TEXT PRIMARY KEY CHECK (name ~ '^[A-Z][A-Z0-9_]{0,127}$'),
    value TEXT NOT NULL CHECK (char_length(value) <= 256),
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
