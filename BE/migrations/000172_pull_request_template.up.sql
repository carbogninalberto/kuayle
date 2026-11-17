-- KUA-210: the template agent pull request descriptions are rendered from,
-- inherited issue -> project -> team -> workspace like the other development
-- settings. NULL uses Kuayle's default template; the API also limits the
-- placeholders a template may use.
ALTER TABLE dev_machine_scope_settings ADD COLUMN pull_request_template text CHECK (
 pull_request_template IS NULL OR (btrim(pull_request_template) <> '' AND char_length(pull_request_template) <= 8000));
