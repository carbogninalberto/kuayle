-- Bounded QA artifacts (reports, screenshots, traces) collected from an
-- independent check's own output directories.
CREATE TABLE agent_validation_artifacts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 step_id uuid NOT NULL REFERENCES dev_machine_agent_run_steps(id) ON DELETE CASCADE,
 path text NOT NULL CHECK (char_length(path) BETWEEN 1 AND 512 AND path !~ '(^/|\.\.)'),
 content_type text NOT NULL CHECK (content_type IN ('image/png','image/jpeg','application/zip','text/html','application/json','application/xml','text/plain','video/webm')),
 size int NOT NULL CHECK (size BETWEEN 0 AND 5242880),
 data bytea NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(step_id, path)
);
