-- Approvals are audit evidence of human decisions.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM conversation_approvals) THEN
  RAISE EXCEPTION 'Cannot downgrade retained assistant approvals';
 END IF;
END $$;
DROP TABLE conversation_approvals;
