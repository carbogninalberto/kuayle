-- The tick of pipeline tasks (KUA-252). A task pinned to a pipeline version
-- says so once, when it starts (a task never changes engine), so the worker
-- and every view know it without reading the version. pipeline_state is the
-- tick's memory between passes: the fact values it last posted to the room,
-- the stall counter, the approval it recorded and the platform actions it
-- already took. The phase is workflow_node, and the status, wake-up, step
-- sequence and budget are the workflow columns graph workflows use.
ALTER TABLE agent_tasks
 ADD COLUMN pipeline boolean NOT NULL DEFAULT false,
 ADD COLUMN pipeline_state jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(pipeline_state) = 'object');
