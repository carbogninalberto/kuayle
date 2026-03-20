ALTER TABLE issues ALTER COLUMN identifier_text TYPE VARCHAR(20);
ALTER TABLE issues ALTER COLUMN status TYPE VARCHAR(20);
ALTER TABLE issues ADD CONSTRAINT issues_status_check CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'cancelled'));
