-- PostgreSQL enum values cannot be removed in place. Retain the inert value,
-- but refuse rollback while publication operations or evidence still exist.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM dev_machine_operations WHERE action::text='publish_agent')
 OR EXISTS(SELECT 1 FROM dev_machine_agent_run_steps WHERE step_type='publication') THEN
  RAISE EXCEPTION 'Cannot downgrade retained agent publication';
 END IF;
END $$;
