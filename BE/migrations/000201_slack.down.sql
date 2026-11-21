-- Earlier PR builds of migration 201 also created this table.
DROP TABLE IF EXISTS slack_threads;
DROP TABLE slack_uninstall_events, slack_identities, slack_bindings, slack_oauth_states;
DROP TABLE slack_installs;
