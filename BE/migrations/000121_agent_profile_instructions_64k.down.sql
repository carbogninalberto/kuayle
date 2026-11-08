-- Longer instructions are cut to 4096 characters (at most 16 KB in UTF-8)
-- before the old limit returns.
UPDATE agent_profiles SET instructions = left(instructions, 4096)
    WHERE octet_length(instructions) > 16384;
ALTER TABLE agent_profiles DROP CONSTRAINT agent_profiles_instructions_check;
ALTER TABLE agent_profiles ADD CONSTRAINT agent_profiles_instructions_check CHECK (octet_length(instructions) <= 16384);
