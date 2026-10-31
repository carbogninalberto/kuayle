-- PostgreSQL enum values cannot be removed in place. Retain the inert value,
-- but refuse rollback while sign-in syncs are still queued or running.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM dev_machine_operations WHERE action::text='sync_agent_sign_ins' AND status IN ('pending','leased')) THEN
  RAISE EXCEPTION 'Cannot downgrade while agent sign-in syncs are queued';
 END IF;
END $$;
