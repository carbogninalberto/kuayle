-- The older schema has no manager that applies image updates; the enum value
-- stays because PostgreSQL cannot drop enum values.
DELETE FROM dev_machine_operations WHERE action = 'update_images';
