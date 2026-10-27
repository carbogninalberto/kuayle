-- Optional workspace commit author for agent publications. Both or neither are
-- set; the email should be verified on the GitHub account to attribute commits.
ALTER TABLE dev_machine_workspace_policies
 ADD COLUMN agent_git_name text CHECK (agent_git_name IS NULL OR (char_length(agent_git_name) BETWEEN 1 AND 100 AND agent_git_name !~ '[[:cntrl:]<>]')),
 ADD COLUMN agent_git_email text CHECK (agent_git_email IS NULL OR (char_length(agent_git_email) <= 254 AND agent_git_email ~ '^[^@[:space:]<>]+@[^@[:space:]<>]+\.[^@[:space:]<>]+$')),
 ADD CONSTRAINT agent_git_author_complete CHECK ((agent_git_name IS NULL) = (agent_git_email IS NULL));
