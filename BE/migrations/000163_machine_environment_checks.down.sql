-- The cached checks describe running containers only: dropping them loses
-- nothing the manager does not find again. Queued refreshes are removed; the
-- enum value stays because PostgreSQL cannot drop enum values.
DROP TABLE IF EXISTS dev_machine_environment_checks;
DELETE FROM dev_machine_operations WHERE action = 'refresh_package_index';
