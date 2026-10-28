-- PostgreSQL enum values cannot be removed in place. Retain the inert value,
-- but refuse rollback while validation operations or evidence still exist.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM dev_machine_operations WHERE action::text='validate_agent')
 OR EXISTS(SELECT 1 FROM dev_machine_agent_run_steps WHERE step_type='validation') THEN
  RAISE EXCEPTION 'Cannot downgrade retained independent validation';
 END IF;
END $$;
