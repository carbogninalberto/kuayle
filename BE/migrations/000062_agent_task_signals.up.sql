-- GitHub CI and review signals for a task attempt's recorded pull request.
-- A signal is recorded once per external event; follow-ups replay a fixed
-- request ID, so webhook redelivery cannot queue a second attempt.
CREATE TABLE agent_task_signals (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 task_id uuid NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
 run_id uuid NOT NULL,
 kind text NOT NULL CHECK (kind IN ('ci','review')),
 external_id text NOT NULL CHECK (char_length(external_id) BETWEEN 1 AND 128),
 outcome text NOT NULL CHECK (outcome ~ '^[a-z_]{1,32}$'),
 name text NOT NULL DEFAULT '' CHECK (char_length(name) <= 200),
 summary text NOT NULL DEFAULT '' CHECK (octet_length(summary) <= 8192),
 url text NOT NULL DEFAULT '' CHECK (char_length(url) <= 2048),
 action text NOT NULL CHECK (action IN ('recorded','follow_up','budget_exhausted','not_reviewable','untrusted','follow_up_failed')),
 request_id uuid NOT NULL,
 follow_up_generation bigint,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(task_id, kind, external_id)
);
CREATE INDEX agent_task_signals_task ON agent_task_signals(task_id, created_at);
