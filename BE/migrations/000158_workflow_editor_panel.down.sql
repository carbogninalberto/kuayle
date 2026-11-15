-- KUA-147: revert the per-user workflow editor panel preference.
ALTER TABLE user_preferences DROP COLUMN IF EXISTS workflow_editor_panel_open;
