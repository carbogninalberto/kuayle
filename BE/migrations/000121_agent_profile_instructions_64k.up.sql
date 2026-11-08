-- Agent profile instructions are Markdown documents now, edited in a side
-- panel with a preview: allow up to 64 KB, like workspace skills.
ALTER TABLE agent_profiles DROP CONSTRAINT agent_profiles_instructions_check;
ALTER TABLE agent_profiles ADD CONSTRAINT agent_profiles_instructions_check CHECK (octet_length(instructions) <= 65536);
