-- The enum value is inert once unused; refuse while purges are still recorded.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM dev_machine_operations WHERE action::text='purge_task_session' AND status IN ('pending','leased')) THEN
  RAISE EXCEPTION 'Cannot downgrade while agent session purges are pending';
 END IF;
END $$;
ALTER TABLE dev_machine_agent_runs DROP COLUMN native_session_id;
