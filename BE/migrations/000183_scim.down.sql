DROP TABLE scim_team_grants, scim_group_members, scim_groups, scim_users, workspace_scim_configs;
ALTER TABLE refresh_tokens DROP COLUMN session_version;
ALTER TABLE users DROP COLUMN session_version;
