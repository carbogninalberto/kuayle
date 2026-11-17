-- Configured templates are dropped; descriptions use the default again.
ALTER TABLE dev_machine_scope_settings DROP COLUMN IF EXISTS pull_request_template;
