-- Skills from the workspace skill library that an agent profile's coding
-- agent gets, in order. Only workspace-wide skills can be attached, so a
-- team's skill never reaches another team's tasks; the repository checks it
-- when the profile is saved. Deleting a profile or a skill drops the link.
CREATE TABLE agent_profile_skills (
    profile_id UUID NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
    skill_id UUID NOT NULL REFERENCES assistant_skills(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    position INT NOT NULL CHECK (position >= 0),
    PRIMARY KEY (profile_id, skill_id)
);
CREATE INDEX agent_profile_skills_skill ON agent_profile_skills (skill_id);
