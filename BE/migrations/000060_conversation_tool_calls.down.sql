-- Tool calls are audit evidence of actions taken on the user's behalf.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM conversation_tool_calls) THEN
  RAISE EXCEPTION 'Cannot downgrade retained assistant tool calls';
 END IF;
END $$;
DROP TABLE conversation_tool_calls;
