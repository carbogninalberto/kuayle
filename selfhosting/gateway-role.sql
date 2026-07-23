\set ON_ERROR_STOP on
\getenv gateway_user GATEWAY_DB_USER
\getenv gateway_password GATEWAY_DB_PASSWORD

SELECT (:'gateway_user' = current_user)::int AS gateway_is_admin \gset
\if :gateway_is_admin
  \echo 'gateway_user must differ from the PostgreSQL administrator'
  \quit 3
\endif

SELECT format('CREATE ROLE %I', :'gateway_user')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'gateway_user') \gexec

SELECT format(
  'ALTER ROLE %I WITH LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT',
  :'gateway_user', :'gateway_password'
) \gexec

SELECT format('REVOKE %I FROM %I', parent.rolname, member.rolname)
FROM pg_auth_members membership
JOIN pg_roles parent ON parent.oid = membership.roleid
JOIN pg_roles member ON member.oid = membership.member
WHERE member.rolname = :'gateway_user' \gexec

SELECT format('REVOKE ALL PRIVILEGES ON DATABASE %I FROM %I', current_database(), :'gateway_user') \gexec
SELECT format('GRANT CONNECT ON DATABASE %I TO %I', current_database(), :'gateway_user') \gexec
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public FROM PUBLIC;
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;
SELECT format('REVOKE ALL PRIVILEGES ON SCHEMA public FROM %I', :'gateway_user') \gexec
SELECT format('GRANT USAGE ON SCHEMA public TO %I', :'gateway_user') \gexec
SELECT format('REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM %I', :'gateway_user') \gexec
SELECT format('REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM %I', :'gateway_user') \gexec
SELECT format('REVOKE ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public FROM %I', :'gateway_user') \gexec

SELECT format('GRANT SELECT ON TABLE dev_machines, dev_machine_services TO %I', :'gateway_user') \gexec
SELECT format('GRANT SELECT (workspace_id, enabled) ON TABLE dev_machine_workspace_policies TO %I', :'gateway_user') \gexec
SELECT format('GRANT SELECT (workspace_id, user_id, role) ON TABLE workspace_members TO %I', :'gateway_user') \gexec
SELECT format(
  'GRANT SELECT (id, workspace_id, machine_id, user_id, status) ON TABLE dev_machine_terminal_sessions TO %I',
  :'gateway_user'
) \gexec
SELECT format('GRANT SELECT ON TABLE dev_machine_access_tickets TO %I', :'gateway_user') \gexec
SELECT format('GRANT UPDATE (status, used_at) ON TABLE dev_machine_access_tickets TO %I', :'gateway_user') \gexec
SELECT format('GRANT SELECT ON TABLE dev_machine_access_sessions TO %I', :'gateway_user') \gexec
SELECT format(
  'GRANT INSERT (id, workspace_id, machine_id, service_id, user_id, token_hash, bound_host, expires_at), UPDATE (last_seen_at) ON TABLE dev_machine_access_sessions TO %I',
  :'gateway_user'
) \gexec
SELECT format('GRANT UPDATE (last_activity_at) ON TABLE dev_machines TO %I', :'gateway_user') \gexec
SELECT format('GRANT SELECT (id, created_at) ON TABLE dev_machine_access_logs TO %I', :'gateway_user') \gexec
SELECT format(
  'GRANT INSERT (workspace_id, machine_id, service_id, user_id, decision, reason, method, path, response_status, remote_ip, user_agent) ON TABLE dev_machine_access_logs TO %I',
  :'gateway_user'
) \gexec
SELECT format('GRANT USAGE ON SEQUENCE dev_machine_access_logs_id_seq TO %I', :'gateway_user') \gexec
SELECT format('GRANT USAGE ON TYPE dev_machine_status TO %I', :'gateway_user') \gexec
