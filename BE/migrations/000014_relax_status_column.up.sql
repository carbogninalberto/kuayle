-- Widen issues.status column and drop CHECK constraint to support custom status slugs
ALTER TABLE issues DROP CONSTRAINT IF EXISTS issues_status_check;
ALTER TABLE issues ALTER COLUMN status TYPE VARCHAR(100);

-- Also widen identifier_text in case team keys + numbers grow
ALTER TABLE issues ALTER COLUMN identifier_text TYPE VARCHAR(50);
