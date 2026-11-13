-- Dropping the delivery markers only re-opens the replay window for deliveries
-- signed in the last days; no user data is lost.
DROP TABLE IF EXISTS forge_webhook_deliveries;
DROP TABLE IF EXISTS forge_commits;
DROP TABLE IF EXISTS forge_branches;
DROP TABLE IF EXISTS forge_merge_requests;
DROP TABLE IF EXISTS forge_repos;
DROP TABLE IF EXISTS forge_connections;
