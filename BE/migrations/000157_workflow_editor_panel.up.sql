-- KUA-147: remember whether the workflow editor's right side panel is open
-- per user, so the collapsed canvas layout survives a reload.
ALTER TABLE user_preferences ADD COLUMN workflow_editor_panel_open BOOLEAN NOT NULL DEFAULT true;
