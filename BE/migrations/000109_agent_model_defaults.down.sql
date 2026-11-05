-- Irreversible for the new effort levels: medium becomes high and xhigh max,
-- the nearest levels the previous release accepts.
DROP TABLE IF EXISTS agent_model_defaults;

UPDATE dev_machine_agent_runs SET thinking = CASE thinking WHEN 'medium' THEN 'high' ELSE 'max' END WHERE thinking IN ('medium', 'xhigh');
UPDATE agent_model_servers SET thinking = CASE thinking WHEN 'medium' THEN 'high' ELSE 'max' END WHERE thinking IN ('medium', 'xhigh');
UPDATE agent_tasks SET thinking = CASE thinking WHEN 'medium' THEN 'high' ELSE 'max' END WHERE thinking IN ('medium', 'xhigh');
ALTER TABLE dev_machine_agent_runs DROP CONSTRAINT dev_machine_agent_runs_thinking_check;
ALTER TABLE dev_machine_agent_runs ADD CONSTRAINT dev_machine_agent_runs_thinking_check
    CHECK (thinking IN ('', 'off', 'low', 'high', 'max'));
ALTER TABLE agent_model_servers DROP CONSTRAINT agent_model_servers_thinking_check;
ALTER TABLE agent_model_servers ADD CONSTRAINT agent_model_servers_thinking_check
    CHECK (thinking IN ('off', 'low', 'high', 'max'));
