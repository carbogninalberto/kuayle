-- Talking to the running agent (KUA-132). Every message a person sends to a
-- task's agent must get a reply: an outbox file that names the message's
-- sequence number (reply_to). deliveries counts the attempts that received a
-- message (a message without a reply is given to the same step's next attempt
-- once more); messages delivered before this migration keep 0 and owe no
-- reply. workflow_node is the step a message was delivered to, so it is never
-- carried to a later step.
ALTER TABLE agent_live_messages
    ADD COLUMN reply_to BIGINT CHECK (reply_to > 0),
    ADD COLUMN deliveries SMALLINT NOT NULL DEFAULT 0 CHECK (deliveries >= 0),
    ADD COLUMN workflow_node TEXT,
    ADD CONSTRAINT agent_live_messages_reply_check CHECK (reply_to IS NULL OR direction = 'from_agent');
CREATE INDEX agent_live_messages_replies ON agent_live_messages(task_id, reply_to) WHERE reply_to IS NOT NULL;

-- The attempt (by generation) a person started to deliver a message: by
-- interrupting the working agent, or by starting one when none ran. While it
-- is the task's generation, the attempt continues the session and is told so;
-- generation+1 while the interrupted attempt is still being stopped.
ALTER TABLE agent_tasks ADD COLUMN live_resume_generation BIGINT;
